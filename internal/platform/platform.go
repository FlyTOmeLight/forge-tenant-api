package platform

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/remote"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

//go:embed schema.sql
var schema string

//go:embed roles.sql
var roleSchema string

type App struct {
	DB       *pgxpool.Pool
	Config   config.Config
	Casdoor  *remote.Casdoor
	GitLab   *remote.GitLab
	HTTP     *http.Client
	OAuth    oauth2.Config
	Verifier *oidc.IDTokenVerifier
	cipher   cipher.AEAD
}
type Object = map[string]any
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Operator bool   `json:"operator"`
	Subject  string `json:"-"`
}

func Token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func ID(prefix string) string { return prefix + "_" + Token()[:24] }
func Hash(s string) string    { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func New(ctx context.Context, c config.Config) (*App, error) {
	key, err := hex.DecodeString(c.AppKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("APP_KEY must be a 32-byte hex key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(c.DatabaseURL)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	cfg.MaxConns = 12
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		db.Close()
		return nil, err
	}
	if c.CAFile != "" {
		pem, e := os.ReadFile(c.CAFile)
		if e != nil {
			db.Close()
			return nil, e
		}
		if !roots.AppendCertsFromPEM(pem) {
			db.Close()
			return nil, errors.New("invalid CA certificate")
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	a := &App{DB: db, Config: c, HTTP: client, cipher: aead, Casdoor: &remote.Casdoor{URL: c.CasdoorURL, ClientID: c.CasdoorClient, Secret: c.CasdoorSecret, Organization: c.Organization, HTTP: client}, GitLab: &remote.GitLab{URL: c.GitLabURL, Token: c.GitLabToken, HTTP: client}}
	return a, nil
}
func (a *App) Migrate(ctx context.Context) error {
	tx, e := a.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(71477231)"); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, schema); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, taskSchema); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, roleSchema); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (a *App) Bootstrap(ctx context.Context) error {
	if a.Config.BootstrapUser == "" {
		return nil
	}
	u, e := a.Casdoor.User(ctx, a.Config.BootstrapUser)
	if e != nil {
		return e
	}
	if u == nil || u.ID == "" || u.Forbidden {
		return errors.New("bootstrap identity missing or disabled")
	}
	_, e = a.DB.Exec(ctx, `INSERT INTO users(id,issuer,subject,username,name,email,status,operator) VALUES($1,$2,$3,$4,$5,$6,'active',true) ON CONFLICT(issuer,subject) DO UPDATE SET operator=true`, ID("u"), a.Config.Issuer, u.ID, u.Name, u.DisplayName, strings.ToLower(u.Email))
	return e
}
func (a *App) seal(b []byte) []byte {
	nonce := make([]byte, a.cipher.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		panic(e)
	}
	return a.cipher.Seal(nonce, nonce, b, nil)
}
func (a *App) open(b []byte) ([]byte, error) {
	n := a.cipher.NonceSize()
	if len(b) < n {
		return nil, errors.New("invalid encrypted result")
	}
	return a.cipher.Open(nil, b[:n], b[n:], nil)
}
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type APIError struct {
	Status        int
	Code, Message string
}

func (e *APIError) Error() string                    { return e.Code }
func problem(status int, code, message string) error { return &APIError{status, code, message} }
func fail(w http.ResponseWriter, e error) {
	var api *APIError
	if errors.As(e, &api) {
		JSON(w, api.Status, Object{"error": Object{"code": api.Code, "message": api.Message}})
		return
	}
	slog.Error("request failed", "error", e.Error())
	JSON(w, 500, Object{"error": Object{"code": "INTERNAL_ERROR", "message": "操作失败，请稍后重试或查看服务日志"}})
}
func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	if e := d.Decode(v); e != nil {
		return problem(400, "INVALID_JSON", "请求内容无效")
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return problem(400, "INVALID_JSON", "仅接受一个 JSON 对象")
	}
	return nil
}
func collect(ctx context.Context, db interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, sql string, args ...any) ([]map[string]any, error) {
	rows, e := db.Query(ctx, sql, args...)
	if e != nil {
		return nil, e
	}
	out, e := pgx.CollectRows(rows, pgx.RowToMap)
	if out == nil {
		out = []map[string]any{}
	}
	return out, e
}
