package account

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/url"

	"golang.org/x/oauth2"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/apperror"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/identity"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) BeginLogin(ctx context.Context, back string) (string, string, error) {
	flow, state, nonce, verifier := identity.Token(), identity.Token(), identity.Token(), oauth2.GenerateVerifier()
	if back != "/invite" {
		back = "/"
	}
	_, e := table.InsertLoginFlow(ctx, a.DB, identity.Hash(flow), identity.Hash(state), nonce, verifier, back)
	if e != nil {
		return "", "", e
	}
	target, e := url.Parse(a.Identity.AuthorizationURL(state, nonce, verifier))
	if e != nil {
		return "", "", e
	}
	// Serve our own authorization page; only the Casdoor JSON endpoints are proxied.
	own, _ := url.Parse(a.Config.PublicURL)
	target.Scheme = own.Scheme
	target.Host = own.Host
	target.Path = "/login/oauth/authorize"
	return flow, target.String(), nil
}

func (a *Service) CompleteLogin(ctx context.Context, flow, state, code string) (string, string, error) {
	var expected, nonce, verifier, back string
	e := table.ConsumeLoginFlow(ctx, a.DB, identity.Hash(flow)).
		Scan(&expected, &nonce, &verifier, &back)
	if e != nil || subtle.ConstantTimeCompare([]byte(expected), []byte(identity.Hash(state))) != 1 || code == "" {
		return "", "", apperror.New(400, "INVALID_LOGIN", "登录请求已失效，请重新登录")
	}
	subject, e := a.Identity.Exchange(ctx, code, verifier, nonce)
	if e != nil {
		var providerError *client.Error
		if errors.As(e, &providerError) {
			return "", "", apperror.New(401, providerError.Code, "身份服务未能完成登录，请重新登录")
		}
		return "", "", e
	}

	var uid string
	e = table.ActiveUserBySubject(ctx, a.DB, a.Config.Casdoor.Issuer, subject).
		Scan(&uid)
	if e != nil {
		return "", "", apperror.New(403, "IDENTITY_NOT_ENROLLED", "此账号尚未在 Forge 中激活")
	}
	session := identity.Token()
	_, e = table.InsertSession(
		ctx,
		a.DB,
		identity.Hash(session),
		uid,
		identity.Hash(identity.Hash(session+":csrf")),
	)
	if e != nil {
		return "", "", e
	}
	return session, back, nil
}

func (a *Service) IdentityLogin(
	ctx context.Context,
	clientID, redirect, kind, responseType, state, flow, method, uri, identitySession string,
	body io.Reader,
) (client.LoginResponse, error) {
	forge := clientID == a.Config.Casdoor.OIDCClientID && redirect == a.Identity.OAuth.RedirectURL
	gitlab := a.Config.Gitlab.ClientID != "" && clientID == a.Config.Gitlab.ClientID &&
		redirect == a.Config.Gitlab.PublicURL+"/users/auth/openid_connect/callback"
	if (!forge && !gitlab) || kind != "code" || responseType != "code" || state == "" {
		return client.LoginResponse{}, apperror.New(400, "INVALID_LOGIN_CLIENT", "登录请求无效")
	}
	if forge {
		if flow == "" {
			return client.LoginResponse{}, apperror.New(400, "INVALID_LOGIN", "请重新发起登录")
		}
		var ok bool
		err := table.LoginFlowExists(ctx, a.DB, identity.Hash(flow), identity.Hash(state)).
			Scan(&ok)
		if err != nil || !ok {
			return client.LoginResponse{}, apperror.New(400, "INVALID_LOGIN", "登录请求已过期")
		}
	}
	response, err := a.Casdoor.ProxyLogin(ctx, a.Config.Casdoor.Issuer, method, uri, identitySession, body)
	if err != nil {
		return client.LoginResponse{}, apperror.New(502, "IDENTITY_UNAVAILABLE", "身份服务暂不可用")
	}
	return response, nil
}
