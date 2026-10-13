package client

import (
	"context"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
)

type OIDC struct {
	OAuth    oauth2.Config
	Verifier *oidc.IDTokenVerifier
	HTTP     *http.Client
}

func (p *OIDC) Initialize(ctx context.Context, c config.Config) error {
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, p.HTTP), c.Casdoor.Issuer)
	if err != nil {
		return err
	}
	p.OAuth = oauth2.Config{
		ClientID:     c.Casdoor.OIDCClientID,
		ClientSecret: c.Casdoor.OIDCClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  c.PublicURL + "/auth/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	p.Verifier = provider.Verifier(&oidc.Config{ClientID: c.Casdoor.OIDCClientID})
	return nil
}

func (p *OIDC) AuthorizationURL(state, nonce, verifier string) string {
	return p.OAuth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
}

func (p *OIDC) Exchange(ctx context.Context, code, verifier, nonce string) (string, error) {
	ctx = oidc.ClientContext(ctx, p.HTTP)
	token, err := p.OAuth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", &Error{Service: "oidc", Status: 401, Code: "TOKEN_EXCHANGE_FAILED"}
	}
	raw, _ := token.Extra("id_token").(string)
	identity, err := p.Verifier.Verify(ctx, raw)
	if err != nil || identity == nil || identity.Subject == "" || identity.Nonce != nonce {
		return "", &Error{Service: "oidc", Status: 401, Code: "INVALID_IDENTITY"}
	}
	return identity.Subject, nil
}
