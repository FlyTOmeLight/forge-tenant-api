package tenant

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/url"

	"golang.org/x/oauth2"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/table"
)

func (a *Service) beginLogin(ctx context.Context, back string) (string, string, error) {
	flow, state, nonce, verifier := Token(), Token(), Token(), oauth2.GenerateVerifier()
	if back != "/invite" {
		back = "/"
	}
	_, e := table.InsertLoginFlow(ctx, a.DB, Hash(flow), Hash(state), nonce, verifier, back)
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

func (a *Service) completeLogin(ctx context.Context, flow, state, code string) (string, string, error) {
	var expected, nonce, verifier, back string
	e := table.ConsumeLoginFlow(ctx, a.DB, Hash(flow)).
		Scan(&expected, &nonce, &verifier, &back)
	if e != nil || subtle.ConstantTimeCompare([]byte(expected), []byte(Hash(state))) != 1 || code == "" {
		return "", "", problem(400, "INVALID_LOGIN", "登录请求已失效，请重新登录")
	}
	subject, e := a.Identity.Exchange(ctx, code, verifier, nonce)
	if e != nil {
		var providerError *client.Error
		if errors.As(e, &providerError) {
			return "", "", problem(401, providerError.Code, "身份服务未能完成登录，请重新登录")
		}
		return "", "", e
	}

	var uid string
	e = table.ActiveUserBySubject(ctx, a.DB, a.Config.Issuer, subject).
		Scan(&uid)
	if e != nil {
		return "", "", problem(403, "IDENTITY_NOT_ENROLLED", "此账号尚未在 Forge 中激活")
	}
	session := Token()
	_, e = table.InsertSession(ctx, a.DB, Hash(session), uid, Hash(Hash(session+":csrf")))
	if e != nil {
		return "", "", e
	}
	return session, back, nil
}

func (a *Service) identityLogin(
	ctx context.Context,
	clientID, redirect, kind, responseType, state, flow, method, uri, identity string,
	body io.Reader,
) (client.LoginResponse, error) {
	forge := clientID == a.Config.ClientID && redirect == a.Identity.OAuth.RedirectURL
	gitlab := a.Config.GitLabClientID != "" && clientID == a.Config.GitLabClientID &&
		redirect == a.Config.GitLabPublicURL+"/users/auth/openid_connect/callback"
	if (!forge && !gitlab) || kind != "code" || responseType != "code" || state == "" {
		return client.LoginResponse{}, problem(400, "INVALID_LOGIN_CLIENT", "登录请求无效")
	}
	if forge {
		if flow == "" {
			return client.LoginResponse{}, problem(400, "INVALID_LOGIN", "请重新发起登录")
		}
		var ok bool
		err := table.LoginFlowExists(ctx, a.DB, Hash(flow), Hash(state)).
			Scan(&ok)
		if err != nil || !ok {
			return client.LoginResponse{}, problem(400, "INVALID_LOGIN", "登录请求已过期")
		}
	}
	response, err := a.Casdoor.ProxyLogin(ctx, a.Config.Issuer, method, uri, identity, body)
	if err != nil {
		return client.LoginResponse{}, problem(502, "IDENTITY_UNAVAILABLE", "身份服务暂不可用")
	}
	return response, nil
}
