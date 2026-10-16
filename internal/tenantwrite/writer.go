package tenantwrite

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

// Writer commits the business change, encrypted replay, audit and synchronization
// outbox in one transaction. All callers use the same tenant lock and version check.
type Writer struct {
	db     *gorm.DB
	cipher cipher.AEAD
}

func New(db *gorm.DB, appKey string) (*Writer, error) {
	key, err := hex.DecodeString(appKey)
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
	return &Writer{db: db, cipher: aead}, nil
}

type Change struct {
	TenantID, ProjectID, MemberID, TeamID, OperationID string
	Remove                                             bool
	Key, Version, Route, Target                        string
	Raw                                                []byte
}

type Receipt struct {
	Status int
	Data   []byte
}

func (a *Writer) Apply(
	ctx context.Context,
	u identity.User,
	action string,
	fn func(*gorm.DB) (map[string]any, error),
	r Change,
) (Receipt, error) {
	tid := r.TenantID
	raw := r.Raw

	key := r.Key
	if key == "" || len(key) > 128 {
		return Receipt{}, apperror.New(400, "IDEMPOTENCY_KEY_REQUIRED", "请提供 1–128 字符的 Idempotency-Key")
	}
	tx := a.db.WithContext(ctx).Begin()
	e := tx.Error
	if e != nil {
		return Receipt{}, e
	}
	defer tx.Rollback()
	lock := tid
	if lock == "" {
		lock = "create-tenant"
	}
	if _, e = table.LockTransaction(ctx, tx, "mutation:"+lock); e != nil {
		return Receipt{}, e
	}
	if action == "create_tenant" || action == "update_tenant" {
		if !u.Operator {
			return Receipt{}, apperror.New(403, "FORBIDDEN", "需要平台管理权限")
		}
	} else {
		var allowed bool
		if action == "set_grant" || action == "revoke_grant" || action == "set_team_grant" ||
			action == "revoke_team_grant" {
			allowed, e = authorization.GrantManager(ctx, tx, u, tid, r.ProjectID)
		} else {
			allowed, e = authorization.CanManage(ctx, tx, u, tid)
		}
		if e != nil {
			return Receipt{}, e
		}
		if !allowed {
			return Receipt{}, apperror.New(404, "NOT_FOUND", "资源不存在或无管理权限")
		}
		if action != "retry_sync" {
			if e = requireActive(ctx, tx, tid); e != nil {
				return Receipt{}, e
			}
		}
	}
	route := r.Route
	digest := identity.Hash(string(raw) + "|" + r.Version)
	record, e := table.FindIdempotency(ctx, tx, u.ID, key)
	oldRoute, oldHash, oldCode, encrypted := record.Route, record.Digest, record.Status, record.Response

	if e == nil {
		if oldRoute != route || oldHash != digest {
			return Receipt{}, apperror.New(409, "IDEMPOTENCY_CONFLICT", "同一请求标识不能用于不同内容")
		}
		decoded, e := a.open(encrypted)
		if e != nil {
			return Receipt{}, e
		}
		return Receipt{Status: oldCode, Data: decoded}, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Receipt{}, e
	}
	if tid != "" {
		var version int64
		e = table.TenantVersion(ctx, tx, tid).Scan(&version)
		if errors.Is(e, sql.ErrNoRows) {
			return Receipt{}, apperror.New(404, "NOT_FOUND", "企业不存在")
		}
		if e != nil {
			return Receipt{}, e
		}
		expected, er := strconv.ParseInt(strings.Trim(r.Version, "\""), 10, 64)
		if er != nil || version != expected {
			return Receipt{}, apperror.New(412, "VERSION_MISMATCH", "数据已变化，请刷新后重试")
		}
	}
	result, e := fn(tx)
	if e != nil {
		var pg interface{ SQLState() string }
		if errors.As(e, &pg) &&
			(pg.SQLState() == "23505" || pg.SQLState() == "23503" || pg.SQLState() == "23514") {
			e = apperror.New(409, "RESOURCE_CONFLICT", "资源已存在或关联关系无效")
		}
		return Receipt{}, e
	}
	if tid == "" {
		tid = result["tenant_id"].(string)
	}
	op, version, e := Enqueue(ctx, tx, tid)
	if e != nil {
		return Receipt{}, e
	}
	detail := map[string]any{}
	for k, v := range result {
		if k != "invitation_url" {
			detail[k] = v
		}
	}
	encodedDetail, e := json.Marshal(detail)
	if e != nil {
		return Receipt{}, e
	}
	if _, e = table.InsertChangeAudit(ctx, tx, tid, u.ID, action, r.Target, encodedDetail, op); e != nil {
		return Receipt{}, e
	}
	if _, e = table.InsertAccessEvent(ctx, tx, tid, version, action); e != nil {
		return Receipt{}, e
	}
	result["operation_id"] = op
	result["version"] = version
	data, e := json.Marshal(result)
	if e != nil {
		return Receipt{}, e
	}
	if _, e = table.InsertIdempotency(ctx, tx, u.ID, key, route, digest, a.seal(data)); e != nil {
		return Receipt{}, e
	}
	if e = tx.Commit().Error; e != nil {
		return Receipt{}, e
	}
	return Receipt{Status: 202, Data: data}, nil
}

func Enqueue(ctx context.Context, tx *gorm.DB, tid string) (string, int64, error) {
	var v int64
	e := table.AdvanceTenantVersion(ctx, tx, tid).Scan(&v)
	if e != nil {
		return "", 0, e
	}
	_, e = table.SupersedeOperations(ctx, tx, tid)
	if e != nil {
		return "", 0, e
	}
	id := identity.ID("op")
	_, e = table.InsertOperation(ctx, tx, id, tid, v)
	return id, v, e
}

func requireActive(ctx context.Context, tx *gorm.DB, tid string) error {
	var active bool
	e := table.TenantActive(ctx, tx, tid).Scan(&active)
	if e != nil {
		return e
	}
	if !active {
		return apperror.New(409, "TENANT_SUSPENDED", "企业已停用，不能增加成员或权限")
	}
	return nil
}

func (a *Writer) seal(b []byte) []byte {
	nonce := make([]byte, a.cipher.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		panic(e)
	}
	return a.cipher.Seal(nonce, nonce, b, nil)
}

func (a *Writer) open(b []byte) ([]byte, error) {
	n := a.cipher.NonceSize()
	if len(b) < n {
		return nil, errors.New("invalid encrypted result")
	}
	return a.cipher.Open(nil, b[:n], b[n:], nil)
}
