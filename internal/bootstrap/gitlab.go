package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

func gitLabSDK(c Config, token string, transport *http.Client) (*gitlab.Client, error) {
	// Mutations are retried only by our stable-name reconciliation, never by HTTP.
	return gitlab.NewClient(
		token,
		gitlab.WithBaseURL(c.GitLabURL+"/api/v4"),
		gitlab.WithHTTPClient(transport),
		gitlab.WithCustomRetryMax(0),
	)
}

func ensureGitLab(ctx context.Context, c Config, store *SecretStore, state *SecretState, transport *http.Client) error {
	adminToken, err := ReadCredential(c.AdminDirectory, "GITLAB_ADMIN_TOKEN")
	if err != nil {
		return err
	}
	admin, err := gitLabSDK(c, adminToken, transport)
	if err != nil {
		return errors.New("cannot initialize GitLab SDK")
	}
	owner, _, err := admin.Users.CurrentUser(gitlab.WithContext(ctx))
	if err != nil || owner == nil || !owner.IsAdmin || owner.State != "active" {
		return errors.New("GitLab bootstrap credential must belong to an active administrator")
	}
	if existing := state.Data["GITLAB_TOKEN"]; existing != "" {
		valid, err := verifyGitLabToken(ctx, c, existing, owner.ID, transport)
		if err != nil {
			return err
		}
		if valid != nil {
			if state.Data["GITLAB_TOKEN_ID"] != "" &&
				state.Data["GITLAB_TOKEN_ID"] != strconv.FormatInt(valid.ID, 10) {
				return errors.New("saved GitLab token ID conflicts with remote identity")
			}
			state.Data["GITLAB_TOKEN_ID"] = strconv.FormatInt(valid.ID, 10)
			state.Data["GITLAB_TOKEN_USER_ID"] = strconv.FormatInt(owner.ID, 10)
			state.Data["GITLAB_TOKEN_NAME"] = valid.Name
			state.Data["GITLAB_ISSUE_ATTEMPTS"] = "0"
			return nil
		}
	}
	// Persist the attempt before issuing. Crashes cannot reset this retry budget.
	attempts, err := strconv.Atoi(state.Data["GITLAB_ISSUE_ATTEMPTS"])
	if state.Data["GITLAB_ISSUE_ATTEMPTS"] == "" {
		attempts = 0
		err = nil
	}
	if err != nil || attempts < 0 {
		return errors.New("invalid GitLab issuance attempt state")
	}
	for attempts < 3 {
		if err := revokeOrphans(
			ctx,
			admin,
			owner.ID,
			c.GitLabTokenName,
			"Forge tenant bootstrap namespace="+c.Namespace,
		); err != nil {
			return err
		}
		attempts++
		state.Data["GITLAB_ISSUE_ATTEMPTS"] = strconv.Itoa(attempts)
		state.Data["BOOTSTRAP_COMPLETE"] = "false"
		if err := store.Save(ctx, state); err != nil {
			return err
		}
		expiry, _ := time.Parse("2006-01-02", c.GitLabTokenExpiresAt)
		date := gitlab.ISOTime(expiry)
		description := "Forge tenant bootstrap namespace=" + c.Namespace
		// The marker distinguishes this integration from unrelated administrator tokens with the same display name.
		token, _, err := admin.Users.CreatePersonalAccessToken(
			owner.ID,
			&gitlab.CreatePersonalAccessTokenOptions{
				Name:        &c.GitLabTokenName,
				Description: &description,
				Scopes:      gitlab.Ptr([]string{"api"}),
				ExpiresAt:   &date,
			},
			gitlab.WithContext(ctx),
		)
		if err != nil || token == nil || token.Token == "" {
			continue
		}
		verified, err := verifyGitLabToken(ctx, c, token.Token, owner.ID, transport)
		if err != nil {
			return err
		}
		if verified == nil {
			return errors.New("new GitLab token is not usable")
		}
		state.Data["GITLAB_TOKEN"] = token.Token
		state.Data["GITLAB_TOKEN_ID"] = strconv.FormatInt(verified.ID, 10)
		state.Data["GITLAB_TOKEN_USER_ID"] = strconv.FormatInt(owner.ID, 10)
		state.Data["GITLAB_TOKEN_NAME"] = verified.Name
		state.Data["GITLAB_ISSUE_ATTEMPTS"] = "0"
		// Save before marking complete. If the response is lost, the next run reads
		// and validates this token first, so it is reused instead of revoked.
		return store.Save(ctx, state)
	}
	// The last ambiguous request may have created one token; reconcile it as well.
	if err := revokeOrphans(
		ctx,
		admin,
		owner.ID,
		c.GitLabTokenName,
		"Forge tenant bootstrap namespace="+c.Namespace,
	); err != nil {
		return err
	}
	return errors.New("GitLab token issuance failed after three attempts; matching orphan tokens revoked")
}

func verifyGitLabToken(
	ctx context.Context,
	c Config,
	token string,
	ownerID int64,
	transport *http.Client,
) (*gitlab.PersonalAccessToken, error) {
	sdk, err := gitLabSDK(c, token, transport)
	if err != nil {
		return nil, errors.New("cannot initialize GitLab SDK")
	}
	info, resp, err := sdk.PersonalAccessTokens.GetSinglePersonalAccessToken(gitlab.WithContext(ctx))
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return nil, nil
		}
		return nil, errors.New("cannot verify saved GitLab token; refusing uncertain replacement")
	}
	if info == nil || info.UserID != ownerID || info.Name != c.GitLabTokenName ||
		info.Description != "Forge tenant bootstrap namespace="+c.Namespace ||
		!slices.Contains(info.Scopes, "api") {
		return nil, errors.New("GitLab token identity or scope conflicts with Forge integration")
	}
	if info.Revoked || !info.Active || (info.ExpiresAt != nil && !time.Time(*info.ExpiresAt).After(time.Now())) {
		return nil, nil
	}
	user, _, err := sdk.Users.CurrentUser(gitlab.WithContext(ctx))
	if err != nil || user == nil || user.ID != ownerID || !user.IsAdmin || user.State != "active" {
		return nil, errors.New("GitLab token lacks active administrator permissions")
	}
	return info, nil
}

func revokeOrphans(ctx context.Context, admin *gitlab.Client, userID int64, name, description string) error {
	// Collect IDs before deleting: deleting while paging would shift later pages.
	var ids []int64
	// GitLab permits a PAT to list only its owner's tokens. The bootstrap token
	// is validated against the active admin identity before this reconciliation.
	options := &gitlab.ListPersonalAccessTokensOptions{
		ListOptions: gitlab.ListOptions{Page: 1, PerPage: 100},
		Search:      &name,
		Revoked:     gitlab.Ptr(false),
	}
	for {
		tokens, response, err := admin.PersonalAccessTokens.ListPersonalAccessTokens(
			options,
			gitlab.WithContext(ctx),
		)
		if err != nil {
			return errors.New("cannot list GitLab bootstrap orphan tokens")
		}
		for _, token := range tokens {
			if token.Name == name && token.Description == description && token.UserID == userID &&
				!token.Revoked {
				ids = append(ids, token.ID)
			}
		}
		if response == nil || response.NextPage == 0 {
			break
		}
		options.Page = response.NextPage
	}
	for _, id := range ids {
		response, err := admin.PersonalAccessTokens.RevokePersonalAccessTokenByID(id, gitlab.WithContext(ctx))
		if err != nil && (response == nil || response.StatusCode != http.StatusNotFound) {
			return errors.New("cannot revoke GitLab bootstrap orphan token")
		}
	}
	return nil
}
