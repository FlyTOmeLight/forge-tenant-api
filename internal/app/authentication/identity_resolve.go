package authentication

import (
	"context"
	"crypto/subtle"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/sdk/go/tenantv1"
)

func (a *Service) ResolveIdentity(
	ctx context.Context,
	in tenantv1.ResolveIdentityRequest,
) (*tenantv1.Principal, error) {
	switch in.Type {
	case tenantv1.IdentityProofSession:
		if in.APIKey != "" {
			return nil, apperror.New(400, "AMBIGUOUS_IDENTITY_PROOF", "身份证明只能使用一种凭据")
		}
		user, err := a.authenticateSessionProof(ctx, in.Session, in.Method, in.Origin, in.CSRF)
		if err != nil {
			return nil, err
		}
		return principal(user), nil
	case tenantv1.IdentityProofAPIKey:
		if in.Session != "" {
			return nil, apperror.New(400, "AMBIGUOUS_IDENTITY_PROOF", "身份证明只能使用一种凭据")
		}
		user, err := a.AuthenticateAPIKey(ctx, in.APIKey)
		if err != nil {
			return nil, err
		}
		return principal(user), nil
	default:
		return nil, apperror.New(400, "INVALID_IDENTITY_PROOF", "身份证明类型无效")
	}
}

func (a *Service) authenticateSessionProof(
	ctx context.Context,
	session, method, origin, csrf string,
) (identity.User, error) {
	user, expected, err := a.Authenticate(ctx, session)
	if err != nil {
		return identity.User{}, err
	}
	if method != "GET" && method != "HEAD" &&
		(origin != a.Config.PublicURL || subtle.ConstantTimeCompare([]byte(csrf), []byte(expected)) != 1) {
		return identity.User{}, apperror.New(403, "CSRF_REJECTED", "请求来源校验失败")
	}
	return user, nil
}

func principal(user identity.User) *tenantv1.Principal {
	return &tenantv1.Principal{ID: user.ID, Name: user.Name, Email: user.Email, Operator: user.Operator}
}
