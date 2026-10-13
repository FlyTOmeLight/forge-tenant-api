package tenant

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

type Object = map[string]any

type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Operator bool   `json:"operator"`
	Subject  string `json:"-"`
}

type Service struct {
	DB       *gorm.DB
	Config   config.Config
	Casdoor  *client.Casdoor
	GitLab   *client.GitLab
	Identity *client.OIDC
	cipher   cipher.AEAD
}

func New(
	db *gorm.DB,
	c config.Config,
	casdoor *client.Casdoor,
	gitlab *client.GitLab,
	identity *client.OIDC,
) (*Service, error) {
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
	return &Service{DB: db, Config: c, Casdoor: casdoor, GitLab: gitlab, Identity: identity, cipher: aead}, nil
}

func (a *Service) Bootstrap(ctx context.Context) error {
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
	_, e = table.UpsertOperator(
		ctx,
		a.DB,
		ID("u"),
		a.Config.Issuer,
		u.ID,
		u.Name,
		u.DisplayName,
		strings.ToLower(u.Email),
	)
	return e
}

func (a *Service) seal(b []byte) []byte {
	nonce := make([]byte, a.cipher.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		panic(e)
	}
	return a.cipher.Seal(nonce, nonce, b, nil)
}

func (a *Service) open(b []byte) ([]byte, error) {
	n := a.cipher.NonceSize()
	if len(b) < n {
		return nil, errors.New("invalid encrypted result")
	}
	return a.cipher.Open(nil, b[:n], b[n:], nil)
}

func (a *Service) Readiness(ctx context.Context) error {
	connection, err := a.DB.DB()
	if err != nil {
		return err
	}
	return connection.PingContext(ctx)
}
