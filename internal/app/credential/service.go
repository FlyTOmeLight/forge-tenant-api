package credential

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

type Service struct{ DB *gorm.DB }

func New(db *gorm.DB) *Service { return &Service{DB: db} }

type Metadata struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	ServiceName string     `json:"service_name,omitempty"`
	UserID      string     `json:"user_id,omitempty"`
}

type Issued struct {
	Metadata
	Token string `json:"token"`
}

var serviceSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

// Expiry applies the shared default and 365-day ceiling for credential creation.
func Expiry(value string, defaultDuration time.Duration) (time.Time, error) {
	now := time.Now().UTC()
	if value == "" {
		return now.Add(defaultDuration), nil
	}
	expiresAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, apperror.New(422, "INVALID_EXPIRY", "到期时间须使用 RFC3339 格式")
	}
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(now) {
		return time.Time{}, apperror.New(422, "INVALID_EXPIRY", "到期时间必须晚于当前时间")
	}
	if expiresAt.After(now.Add(365 * 24 * time.Hour)) {
		return time.Time{}, apperror.New(422, "INVALID_EXPIRY", "到期时间最远为 365 天")
	}
	return expiresAt, nil
}

func (a *Service) IssueUser(
	ctx context.Context,
	userID string,
	name string,
	expiresAt time.Time,
) (*Issued, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	token, hash := newToken()
	id := identity.ID("cr")
	if _, err := table.InsertCredential(
		ctx, a.DB, id, "user", userID, "", name, hash, expiresAt,
	); err != nil {
		return nil, err
	}
	return &Issued{
		Metadata: Metadata{
			ID:        id,
			Name:      name,
			CreatedAt: time.Now().UTC(),
			ExpiresAt: expiresAt,
			UserID:    userID,
		},
		Token: token,
	}, nil
}

func (a *Service) IssueService(
	ctx context.Context,
	service string,
	name string,
	expiresAt time.Time,
) (*Issued, error) {
	if !serviceSlug.MatchString(service) {
		return nil, apperror.New(422, "INVALID_SERVICE_NAME", "服务名须为 2–64 位小写字母、数字或连字符")
	}
	if err := validateName(name); err != nil {
		return nil, err
	}
	token, hash := newToken()
	id := identity.ID("cr")
	if _, err := table.InsertCredential(
		ctx, a.DB, id, "service", "", service, name, hash, expiresAt,
	); err != nil {
		return nil, err
	}
	return &Issued{
		Metadata: Metadata{
			ID: id, Name: name, CreatedAt: time.Now().UTC(), ExpiresAt: expiresAt, ServiceName: service,
		},
		Token: token,
	}, nil
}

// EnsureService registers a pre-generated token without returning or persisting plaintext.
// An existing row is never extended, reassigned, or reactivated.
func (a *Service) EnsureService(ctx context.Context, service, name, token, expiry string) (Metadata, error) {
	if !serviceSlug.MatchString(service) {
		return Metadata{}, apperror.New(422, "INVALID_SERVICE_NAME", "服务名须为 2–64 位小写字母、数字或连字符")
	}
	if err := validateName(name); err != nil {
		return Metadata{}, err
	}
	if !strings.HasPrefix(token, "ftk_") {
		return Metadata{}, apperror.New(422, "INVALID_TOKEN", "invalid service token format")
	}
	secret, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(token, "ftk_"))
	if err != nil || len(secret) != 32 {
		return Metadata{}, apperror.New(422, "INVALID_TOKEN", "invalid service token format")
	}
	hash := identity.Hash(token)
	var result Metadata
	err = a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows, err := table.CredentialsForEnsure(ctx, tx, service, name, hash)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.SecretHash != hash || row.OwnerKind != "service" || row.ServiceName == nil || *row.ServiceName != service || row.Name != name {
				return apperror.New(409, "CREDENTIAL_CONFLICT", "credential token or owner conflicts with existing registration")
			}
			if row.RevokedAt != nil || !row.ExpiresAt.After(time.Now()) {
				return apperror.New(409, "CREDENTIAL_INACTIVE", "registered credential is revoked or expired")
			}
			result = metadata(row)
		}
		if len(rows) > 0 {
			return nil
		}
		expiresAt, err := Expiry(expiry, 365*24*time.Hour)
		if err != nil {
			return err
		}
		id := identity.ID("cr")
		if _, err := table.InsertCredential(ctx, tx, id, "service", "", service, name, hash, expiresAt); err != nil {
			return err
		}
		result = Metadata{ID: id, Name: name, CreatedAt: time.Now().UTC(), ExpiresAt: expiresAt, ServiceName: service}
		return nil
	})
	return result, err
}

func (a *Service) ListUser(ctx context.Context, userID string) ([]Metadata, error) {
	rows, err := table.ListUserCredentials(ctx, a.DB, userID)
	if err != nil {
		return nil, err
	}
	items := make([]Metadata, 0, len(rows))
	for _, row := range rows {
		items = append(items, metadata(row))
	}
	return items, nil
}

func (a *Service) RevokeUser(ctx context.Context, userID, id string) error {
	affected, err := table.RevokeUserCredential(ctx, a.DB, userID, id)
	if err != nil {
		return err
	}
	if affected == 0 {
		return apperror.New(404, "NOT_FOUND", "凭据不存在")
	}
	return nil
}

func (a *Service) ListService(ctx context.Context, service string) ([]Metadata, error) {
	rows, err := table.ListServiceCredentials(ctx, a.DB, service)
	if err != nil {
		return nil, err
	}
	items := make([]Metadata, 0, len(rows))
	for _, row := range rows {
		items = append(items, metadata(row))
	}
	return items, nil
}

func (a *Service) RevokeService(ctx context.Context, id string) (bool, error) {
	affected, err := table.RevokeServiceCredential(ctx, a.DB, id)
	return affected != 0, err
}

func metadata(row table.Credential) Metadata {
	item := Metadata{
		ID: row.ID, Name: row.Name, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt,
		RevokedAt: row.RevokedAt,
	}
	if row.UserID != nil {
		item.UserID = *row.UserID
	}
	if row.ServiceName != nil {
		item.ServiceName = *row.ServiceName
	}
	return item
}

func validateName(name string) error {
	name = strings.TrimSpace(name)
	if len([]rune(name)) < 1 || len([]rune(name)) > 80 {
		return apperror.New(422, "INVALID_CREDENTIAL_NAME", "凭据名称须为 1–80 个字符")
	}
	return nil
}

func newToken() (string, string) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic(err)
	}
	token := "ftk_" + base64.RawURLEncoding.EncodeToString(secret)
	return token, identity.Hash(token)
}
