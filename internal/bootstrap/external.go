// Package bootstrap provisions the dedicated Forge integration using official APIs.
package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
)

// Config contains public parameters and mounted file paths, never credentials.
type Config struct {
	CasdoorURL, CasdoorOrganization, CasdoorCertificate            string
	OIDCApplication, ManagementApplication, PublicURL              string
	BootstrapUsername, BootstrapEmail                              string
	GitLabURL, GitLabTokenName, GitLabTokenExpiresAt               string
	Namespace, OutputSecret, AdminDirectory, SeedDirectory, CAFile string
	TimeoutSeconds                                                 int
}

var (
	RequiredKeys = []string{
		"CASDOOR_CLIENT_ID",
		"CASDOOR_SECRET",
		"OIDC_CLIENT_ID",
		"OIDC_CLIENT_SECRET",
		"GITLAB_TOKEN",
		"BOOTSTRAP_USERNAME",
	}
	stableName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
)

func (c Config) Validate() error {
	for _, value := range []string{c.Namespace, c.OutputSecret, c.CasdoorOrganization, c.OIDCApplication, c.ManagementApplication, c.BootstrapUsername, c.GitLabTokenName} {
		if !stableName.MatchString(value) {
			return errors.New("bootstrap names must be 2–63 lowercase letters, digits or hyphens")
		}
	}
	if c.OIDCApplication == c.ManagementApplication {
		return errors.New("OIDC and management applications must be distinct")
	}
	// The lab's Casdoor and GitLab services are plain HTTP inside the cluster
	// (user-approved for this deployment). URL shape is still restricted.
	if !validServiceOrigin(c.CasdoorURL) || !validServiceOrigin(c.GitLabURL) {
		return errors.New("bootstrap service URLs must be HTTP(S) origins without credentials, query or path")
	}
	if !validPublicOrigin(c.PublicURL) {
		return errors.New("PublicURL must be an HTTPS origin or loopback HTTP origin")
	}
	if c.CasdoorCertificate == "" || c.BootstrapEmail == "" || c.AdminDirectory == "" || c.SeedDirectory == "" {
		return errors.New("bootstrap requires certificate, email and mounted credential directories")
	}
	expiry, err := time.Parse("2006-01-02", c.GitLabTokenExpiresAt)
	if err != nil || !expiry.After(time.Now()) || expiry.After(time.Now().Add(365*24*time.Hour)) {
		return errors.New("GitLabTokenExpiresAt must be a future YYYY-MM-DD within 365 days")
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 600 {
		return errors.New("TimeoutSeconds must be 1–600")
	}
	return nil
}

func validServiceOrigin(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		(u.Path == "" || u.Path == "/") && (u.Scheme == "https" || u.Scheme == "http")
}

func validPublicOrigin(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		(u.Path == "" || u.Path == "/") &&
		(u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost")))
}

func ReadCredential(directory, key string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(directory, key))
	if err != nil || strings.TrimSpace(string(raw)) == "" {
		return "", fmt.Errorf("missing mounted credential %s", key)
	}
	return strings.TrimSpace(string(raw)), nil
}

func Run(ctx context.Context, c Config, store *SecretStore, httpClient *http.Client) error {
	if err := c.Validate(); err != nil {
		return err
	}
	state, err := store.Acquire(ctx, time.Duration(c.TimeoutSeconds)*time.Second+time.Minute)
	if err != nil {
		return err
	}
	defer store.Release(context.WithoutCancel(ctx))
	seed := map[string]string{}
	for _, key := range []string{"CASDOOR_CLIENT_ID", "CASDOOR_SECRET", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "BOOTSTRAP_PASSWORD"} {
		seed[key], err = ReadCredential(c.SeedDirectory, key)
		if err != nil {
			return err
		}
	}
	if seed["CASDOOR_CLIENT_ID"] == seed["OIDC_CLIENT_ID"] || len(seed["CASDOOR_SECRET"]) < 32 ||
		len(seed["OIDC_CLIENT_SECRET"]) < 32 ||
		len(seed["BOOTSTRAP_PASSWORD"]) < 16 {
		return errors.New("independent client IDs and strong generated secrets are required")
	}
	adminID, err := ReadCredential(c.AdminDirectory, "CASDOOR_ADMIN_CLIENT_ID")
	if err != nil {
		return err
	}
	adminSecret, err := ReadCredential(c.AdminDirectory, "CASDOOR_ADMIN_CLIENT_SECRET")
	if err != nil {
		return err
	}
	if seed["CASDOOR_CLIENT_ID"] == adminID || seed["OIDC_CLIENT_ID"] == adminID {
		return errors.New("bootstrap clients must be distinct from administrator client")
	}
	admin := &client.Casdoor{
		URL:          c.CasdoorURL,
		ClientID:     adminID,
		Secret:       adminSecret,
		Organization: c.CasdoorOrganization,
		HTTP:         httpClient,
	}
	for _, app := range []*casdoorsdk.Application{
		{Owner: "admin", Name: c.OIDCApplication, DisplayName: c.OIDCApplication, Organization: c.CasdoorOrganization, Cert: c.CasdoorCertificate, ClientId: seed["OIDC_CLIENT_ID"], ClientSecret: seed["OIDC_CLIENT_SECRET"], RedirectUris: []string{strings.TrimRight(c.PublicURL, "/") + "/auth/callback"}, GrantTypes: []string{"authorization_code", "refresh_token"}, EnablePassword: true, EnableSigninSession: true, TokenFormat: "JWT", TokenSigningMethod: "RS256", ExpireInHours: 1, RefreshExpireInHours: 24},
		{Owner: "admin", Name: c.ManagementApplication, DisplayName: c.ManagementApplication, Organization: c.CasdoorOrganization, Cert: c.CasdoorCertificate, ClientId: seed["CASDOOR_CLIENT_ID"], ClientSecret: seed["CASDOOR_SECRET"], GrantTypes: []string{"client_credentials"}, DisableSignin: true, TokenFormat: "JWT", TokenSigningMethod: "RS256", ExpireInHours: 1},
	} {
		if err := ensureApplication(ctx, admin, app); err != nil {
			return err
		}
	}
	management := &client.Casdoor{
		URL:          c.CasdoorURL,
		ClientID:     seed["CASDOOR_CLIENT_ID"],
		Secret:       seed["CASDOOR_SECRET"],
		Organization: c.CasdoorOrganization,
		HTTP:         httpClient,
	}
	// Creating (or re-submitting) our exact user through the new management client
	// proves write authorization even when a prior run created the user already.
	digest := sha256.Sum256(
		[]byte(c.CasdoorOrganization + "/" + c.ManagementApplication + "/" + c.BootstrapUsername),
	)
	userID := hex.EncodeToString(digest[:16])
	query := url.Values{"id": {c.CasdoorOrganization + "/" + c.BootstrapUsername}}
	var existing *casdoorsdk.User
	if err := management.Call(ctx, "get-user", query, nil, &existing); err != nil {
		return errors.New("new Casdoor management client cannot query users")
	}
	user := &casdoorsdk.User{
		Owner:       c.CasdoorOrganization,
		Name:        c.BootstrapUsername,
		Id:          userID,
		DisplayName: c.BootstrapUsername,
		Email:       c.BootstrapEmail,
		Password:    seed["BOOTSTRAP_PASSWORD"],
		Type:        "normal-user",
	}
	if existing != nil {
		if existing.Id != userID || existing.Owner != user.Owner || existing.Email != user.Email ||
			existing.IsForbidden ||
			existing.IsDeleted {
			return errors.New("bootstrap user identity conflicts or is disabled")
		}
		// add-user uses the official API and checks authorization before detecting duplicates.
		// Verify write rights via an unchanged update, without resetting passwords.
		if err := management.Call(ctx, "update-user", query, existing, nil); err != nil {
			return errors.New("new Casdoor management client cannot update Forge user")
		}
	} else if err := management.Call(ctx, "add-user", nil, user, nil); err != nil {
		return errors.New("new Casdoor management client cannot create Forge user")
	}
	var verified *casdoorsdk.User
	if err := management.Call(
		ctx,
		"get-user",
		query,
		nil,
		&verified,
	); err != nil || verified == nil || verified.Id != userID ||
		verified.IsForbidden {
		return errors.New("Casdoor management user verification failed")
	}
	for _, key := range []string{"CASDOOR_CLIENT_ID", "CASDOOR_SECRET", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET"} {
		if state.Data[key] != "" && state.Data[key] != seed[key] {
			return errors.New("external credentials conflict with stable identity seed")
		}
		state.Data[key] = seed[key]
	}
	state.Data["BOOTSTRAP_USERNAME"] = c.BootstrapUsername
	if err := ensureGitLab(ctx, c, store, state, httpClient); err != nil {
		return err
	}
	state.Data["BOOTSTRAP_COMPLETE"] = "true"
	return store.Save(ctx, state)
}

func ensureApplication(ctx context.Context, admin *client.Casdoor, wanted *casdoorsdk.Application) error {
	query := url.Values{"id": {"admin/" + wanted.Name}}
	var existing *casdoorsdk.Application
	if err := admin.Call(ctx, "get-application", query, nil, &existing); err != nil {
		return errors.New("cannot query Casdoor application")
	}
	if existing == nil {
		if err := admin.Call(ctx, "add-application", nil, wanted, nil); err != nil {
			return errors.New("cannot create Casdoor application; retry will inspect stable name")
		}
		if err := admin.Call(ctx, "get-application", query, nil, &existing); err != nil {
			return errors.New("cannot verify Casdoor application")
		}
	}
	if existing == nil || existing.Owner != wanted.Owner || existing.Organization != wanted.Organization ||
		existing.ClientId != wanted.ClientId ||
		existing.ClientSecret != wanted.ClientSecret ||
		existing.Cert != wanted.Cert ||
		!slices.Equal(existing.RedirectUris, wanted.RedirectUris) ||
		!slices.Equal(existing.GrantTypes, wanted.GrantTypes) ||
		existing.TokenFormat != wanted.TokenFormat ||
		existing.TokenSigningMethod != wanted.TokenSigningMethod ||
		existing.DisableSignin != wanted.DisableSignin ||
		existing.EnablePassword != wanted.EnablePassword {
		return errors.New("Casdoor application conflicts with dedicated Forge configuration")
	}
	return nil
}
