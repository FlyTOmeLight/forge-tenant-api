package authorization

import (
	"context"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func (a *Service) CheckAccess(
	ctx context.Context,
	in tenantv1.CheckAccessRequest,
) (*tenantv1.CheckAccessResponse, error) {
	if in.Action != "read" && in.Action != "write" && in.Action != "manage" {
		return nil, apperror.New(422, "INVALID_ACTION", "action 必须为 read、write 或 manage")
	}
	v, e := a.Access(ctx, in.UserID, in.TenantID, in.ProjectID)
	if e != nil {
		return nil, e
	}
	switch in.Action {
	case tenantv1.AccessRead:
		v.Allowed = v.Read
	case tenantv1.AccessWrite:
		v.Allowed = v.Write
	case tenantv1.AccessManage:
		v.Allowed = v.Manage
	}
	return v, nil
}
