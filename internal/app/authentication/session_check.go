package authentication

import (
	"context"
	"crypto/subtle"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func (a *Service) CheckSession(
	ctx context.Context,
	in tenantv1.CheckSessionRequest,
) (*tenantv1.CheckSessionResponse, error) {
	u, csrf, e := a.Authenticate(ctx, in.Session)
	if e != nil {
		return nil, e
	}
	if in.Method != "GET" && in.Method != "HEAD" {
		if in.Origin != a.Config.PublicURL || subtle.ConstantTimeCompare([]byte(in.CSRF), []byte(csrf)) != 1 {
			return nil, apperror.New(403, "CSRF_REJECTED", "请求来源校验失败")
		}
	}
	return &tenantv1.CheckSessionResponse{ID: u.ID, Name: u.Name, Email: u.Email, Operator: u.Operator}, nil
}
