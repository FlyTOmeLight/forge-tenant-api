package table

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
)

type Credential struct {
	ID          string     `gorm:"column:id;type:text;primaryKey;autoIncrement:false"`
	OwnerKind   string     `gorm:"column:owner_kind;type:text;not null;check:credentials_owner_kind_check,owner_kind IN ('user','service')"`
	UserID      *string    `gorm:"column:user_id;type:text;check:credentials_owner_check,(owner_kind = 'user' AND user_id IS NOT NULL AND service_name IS NULL) OR (owner_kind = 'service' AND user_id IS NULL AND service_name IS NOT NULL)"`
	ServiceName *string    `gorm:"column:service_name;type:text"`
	Name        string     `gorm:"column:name;type:text;not null"`
	SecretHash  string     `gorm:"column:secret_hash;type:text;not null;uniqueIndex"`
	CreatedAt   time.Time  `gorm:"column:created_at;type:timestamptz;not null;default:now();autoCreateTime:false;autoUpdateTime:false"`
	ExpiresAt   time.Time  `gorm:"column:expires_at;type:timestamptz;not null"`
	RevokedAt   *time.Time `gorm:"column:revoked_at;type:timestamptz"`
	User        *User      `gorm:"foreignKey:UserID;references:ID;constraint:credentials_user_id_fkey,OnUpdate:NO ACTION,OnDelete:NO ACTION"`
}

func (Credential) TableName() string { return "credentials" }

func InsertCredential(
	ctx context.Context,
	db *gorm.DB,
	id, ownerKind, userID, serviceName, name, secretHash string,
	expiresAt time.Time,
) (int64, error) {
	result := db.WithContext(ctx).Exec(
		`INSERT INTO credentials(id,owner_kind,user_id,service_name,name,secret_hash,created_at,expires_at) VALUES(?,?,?,?,?,?,now(),?)`,
		id, ownerKind, nullString(userID), nullString(serviceName), name, secretHash, expiresAt,
	)
	return result.RowsAffected, result.Error
}

// CredentialsForEnsure serializes bootstrap registration within the caller's transaction.
func CredentialsForEnsure(ctx context.Context, tx *gorm.DB, service, name, hash string) ([]Credential, error) {
	if err := tx.WithContext(ctx).
		Exec(`SELECT pg_advisory_xact_lock(hashtext('forge-service-credential-ensure'))`).
		Error; err != nil {
		return nil, err
	}
	var rows []Credential
	err := tx.WithContext(ctx).
		Where("secret_hash=? OR (owner_kind='service' AND service_name=? AND name=?)", hash, service, name).
		Find(&rows).
		Error
	return rows, err
}

func UserByCredentialHash(ctx context.Context, db *gorm.DB, secretHash string) (identity.User, error) {
	var user identity.User
	err := db.WithContext(ctx).
		Raw(`SELECT u.id,u.name,u.email,u.operator,coalesce(u.subject,'') FROM credentials c JOIN users u ON u.id=c.user_id WHERE c.secret_hash=? AND c.owner_kind='user' AND c.revoked_at IS NULL AND c.expires_at>now() AND u.status='active'`, secretHash).
		Row().
		Scan(&user.ID, &user.Name, &user.Email, &user.Operator, &user.Subject)
	return user, err
}

func ServiceByCredentialHash(ctx context.Context, db *gorm.DB, secretHash string) (string, error) {
	var service string
	err := db.WithContext(ctx).
		Raw(`SELECT service_name FROM credentials WHERE secret_hash=? AND owner_kind='service' AND revoked_at IS NULL AND expires_at>now()`, secretHash).
		Row().
		Scan(&service)
	return service, err
}

func ListUserCredentials(ctx context.Context, db *gorm.DB, userID string) ([]Credential, error) {
	var rows []Credential
	err := db.WithContext(ctx).
		Where("owner_kind='user' AND user_id=?", userID).
		Order("created_at DESC").
		Limit(200).
		Find(&rows).Error
	return rows, err
}

func ListServiceCredentials(ctx context.Context, db *gorm.DB, service string) ([]Credential, error) {
	query := db.WithContext(ctx).Where("owner_kind='service'")
	if service != "" {
		query = query.Where("service_name=?", service)
	}
	var rows []Credential
	err := query.Order("created_at DESC").Limit(500).Find(&rows).Error
	return rows, err
}

func RevokeUserCredential(ctx context.Context, db *gorm.DB, userID, id string) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE credentials SET revoked_at=coalesce(revoked_at,now()) WHERE id=? AND owner_kind='user' AND user_id=?`, id, userID)
	return result.RowsAffected, result.Error
}

func RevokeServiceCredential(ctx context.Context, db *gorm.DB, id string) (int64, error) {
	result := db.WithContext(ctx).
		Exec(`UPDATE credentials SET revoked_at=now() WHERE id=? AND owner_kind='service' AND revoked_at IS NULL`, id)
	return result.RowsAffected, result.Error
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
