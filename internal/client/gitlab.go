package client

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

type GitLab struct {
	URL, Token, TokenFile string
	HTTP                  *http.Client
}

// gitLabCredentialSource lets the SDK obtain the current projected Secret for
// every request, including calls made within a multi-step synchronization.
type gitLabCredentialSource struct{ client *GitLab }

func (gitLabCredentialSource) Init(context.Context, *gitlab.Client) error { return nil }

func (s gitLabCredentialSource) Header(context.Context) (string, string, error) {
	token, err := s.client.Credential()
	return "PRIVATE-TOKEN", token, err
}

func (g *GitLab) sdk() (*gitlab.Client, error) {
	options := []gitlab.ClientOptionFunc{
		gitlab.WithBaseURL(strings.TrimRight(g.URL, "/") + "/api/v4"),
		gitlab.WithoutRetries(),
	}
	if g.HTTP != nil {
		options = append(options, gitlab.WithHTTPClient(g.HTTP))
	}
	return gitlab.NewAuthSourceClient(gitLabCredentialSource{client: g}, options...)
}

func gitLabError(response *gitlab.Response, err error) error {
	if err == nil {
		return nil
	}
	var local *Error
	if errors.As(err, &local) {
		return local
	}
	if response == nil {
		return &Error{Service: "gitlab", Code: "UNAVAILABLE"}
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return &Error{Service: "gitlab", Status: 502, Code: "INVALID_RESPONSE"}
	}
	var delay time.Duration
	if seconds, e := strconv.Atoi(response.Header.Get("Retry-After")); e == nil {
		delay = time.Duration(seconds) * time.Second
	} else if date, e := http.ParseTime(response.Header.Get("Retry-After")); e == nil {
		delay = time.Until(date)
	}
	return &Error{Service: "gitlab", Status: response.StatusCode, Code: "HTTP_ERROR", RetryAfter: delay}
}

func (g *GitLab) Version(ctx context.Context) (map[string]any, error) {
	sdk, err := g.sdk()
	if err != nil {
		return nil, err
	}
	version, response, err := sdk.Version.GetVersion(gitlab.WithContext(ctx))
	if err != nil {
		return nil, gitLabError(response, err)
	}
	if version == nil {
		return nil, &Error{Service: "gitlab", Status: 502, Code: "INVALID_RESPONSE"}
	}
	return map[string]any{"version": version.Version, "revision": version.Revision}, nil
}

func (g *GitLab) EnsureGroup(ctx context.Context, id, slug, name string, remoteID int64) (int64, error) {
	sdk, err := g.sdk()
	if err != nil {
		return 0, err
	}
	marker := "managed-by:forge-tenant;tenant:" + id
	var key any = slug
	if remoteID != 0 {
		key = remoteID
	}
	group, response, err := sdk.Groups.GetGroup(key, nil, gitlab.WithContext(ctx))
	err = gitLabError(response, err)
	if IsStatus(err, 404) && remoteID == 0 {
		group, response, err = sdk.Groups.CreateGroup(
			&gitlab.CreateGroupOptions{
				Name:        gitlab.Ptr(name),
				Path:        gitlab.Ptr(slug),
				Visibility:  gitlab.Ptr(gitlab.PrivateVisibility),
				Description: gitlab.Ptr(marker),
			},
			gitlab.WithContext(ctx),
		)
		err = gitLabError(response, err)
	}
	if err != nil {
		return 0, err
	}
	if group == nil {
		return 0, &Error{Service: "gitlab", Status: 502, Code: "INVALID_RESPONSE"}
	}
	if group.Description != marker || group.FullPath != slug || group.Visibility != gitlab.PrivateVisibility ||
		len(group.SharedWithGroups) > 0 {
		return 0, Conflict("gitlab", "GROUP_OWNERSHIP_OR_VISIBILITY")
	}
	return group.ID, nil
}

func (g *GitLab) EnsureUser(ctx context.Context, subject, username, name, email string) (int64, error) {
	sdk, err := g.sdk()
	if err != nil {
		return 0, err
	}
	users, response, err := sdk.Users.ListUsers(
		&gitlab.ListUsersOptions{Provider: gitlab.Ptr("openid_connect"), ExternalUID: gitlab.Ptr(subject)},
		gitlab.WithContext(ctx),
	)
	if err != nil {
		return 0, gitLabError(response, err)
	}
	if len(users) > 1 {
		return 0, Conflict("gitlab", "DUPLICATE_EXTERNAL_IDENTITY")
	}
	var user *gitlab.User
	if len(users) == 0 {
		user, response, err = sdk.Users.CreateUser(
			&gitlab.CreateUserOptions{
				Username:            gitlab.Ptr(username),
				Name:                gitlab.Ptr(name),
				Email:               gitlab.Ptr(email),
				ForceRandomPassword: gitlab.Ptr(true),
				SkipConfirmation:    gitlab.Ptr(true),
				Provider:            gitlab.Ptr("openid_connect"),
				ExternUID:           gitlab.Ptr(subject),
			},
			gitlab.WithContext(ctx),
		)
		if err != nil {
			return 0, gitLabError(response, err)
		}
	} else {
		user = users[0]
	}
	if user == nil {
		return 0, &Error{Service: "gitlab", Status: 502, Code: "INVALID_RESPONSE"}
	}
	// Filtered responses may omit administrative fields: read the full user.
	user, response, err = sdk.Users.GetUser(user.ID, nil, gitlab.WithContext(ctx))
	if err != nil {
		return 0, gitLabError(response, err)
	}
	if user == nil {
		return 0, &Error{Service: "gitlab", Status: 502, Code: "INVALID_RESPONSE"}
	}
	if user.IsAdmin || user.State != "active" {
		return 0, Conflict("gitlab", "USER_ADMIN_OR_INACTIVE")
	}
	bound := false
	for _, identity := range user.Identities {
		if identity != nil && identity.Provider == "openid_connect" && identity.ExternUID == subject {
			bound = true
		}
	}
	if !bound {
		return 0, Conflict("gitlab", "IDENTITY_BINDING_MISMATCH")
	}
	return user.ID, nil
}

func (g *GitLab) EnsureProject(
	ctx context.Context,
	id, path, name, mode string,
	groupID, remoteID int64,
) (int64, error) {
	sdk, err := g.sdk()
	if err != nil {
		return 0, err
	}
	marker := "managed-by:forge-tenant;project:" + id
	var key any = path
	if remoteID != 0 {
		key = remoteID
	}
	project, response, err := sdk.Projects.GetProject(key, nil, gitlab.WithContext(ctx))
	err = gitLabError(response, err)
	if IsStatus(err, 404) && remoteID == 0 && mode == "create" {
		project, response, err = sdk.Projects.CreateProject(
			&gitlab.CreateProjectOptions{
				Name:                 gitlab.Ptr(name),
				Path:                 gitlab.Ptr(path[strings.LastIndex(path, "/")+1:]),
				NamespaceID:          gitlab.Ptr(groupID),
				Visibility:           gitlab.Ptr(gitlab.PrivateVisibility),
				Description:          gitlab.Ptr(marker),
				InitializeWithReadme: gitlab.Ptr(true),
			},
			gitlab.WithContext(ctx),
		)
		err = gitLabError(response, err)
	}
	if err != nil {
		return 0, err
	}
	if project == nil {
		return 0, &Error{Service: "gitlab", Status: 502, Code: "INVALID_RESPONSE"}
	}
	if project.Namespace == nil || project.Namespace.ID != groupID || project.PathWithNamespace != path ||
		project.Visibility != gitlab.PrivateVisibility ||
		len(project.SharedWithGroups) > 0 ||
		(mode == "create" && project.Description != marker) {
		return 0, Conflict("gitlab", "PROJECT_SCOPE_OR_VISIBILITY")
	}
	return project.ID, nil
}

func (g *GitLab) Access(ctx context.Context, projectID, userID int64, all bool) (int, error) {
	sdk, err := g.sdk()
	if err != nil {
		return 0, err
	}
	var member *gitlab.ProjectMember
	var response *gitlab.Response
	if all {
		member, response, err = sdk.ProjectMembers.GetInheritedProjectMember(
			projectID,
			userID,
			gitlab.WithContext(ctx),
		)
	} else {
		member, response, err = sdk.ProjectMembers.GetProjectMember(projectID, userID, gitlab.WithContext(ctx))
	}
	err = gitLabError(response, err)
	if IsStatus(err, 404) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if member == nil {
		return 0, &Error{Service: "gitlab", Status: 502, Code: "INVALID_RESPONSE"}
	}
	return int(member.AccessLevel), nil
}

func (g *GitLab) SetAccess(ctx context.Context, projectID, userID int64, level int) (int, error) {
	direct, err := g.Access(ctx, projectID, userID, false)
	if err != nil {
		return 0, err
	}
	sdk, err := g.sdk()
	if err != nil {
		return 0, err
	}
	var response *gitlab.Response
	switch {
	case level == 0 && direct > 0:
		response, err = sdk.ProjectMembers.DeleteProjectMember(projectID, userID, gitlab.WithContext(ctx))
		err = gitLabError(response, err)
		if IsStatus(err, 404) {
			err = nil
		}
	case level > 0 && direct == 0:
		_, response, err = sdk.ProjectMembers.AddProjectMember(
			projectID,
			&gitlab.AddProjectMemberOptions{
				UserID:      userID,
				AccessLevel: gitlab.Ptr(gitlab.AccessLevelValue(level)),
			},
			gitlab.WithContext(ctx),
		)
		err = gitLabError(response, err)
	case level > 0 && direct != level:
		_, response, err = sdk.ProjectMembers.EditProjectMember(
			projectID,
			userID,
			&gitlab.EditProjectMemberOptions{AccessLevel: gitlab.Ptr(gitlab.AccessLevelValue(level))},
			gitlab.WithContext(ctx),
		)
		err = gitLabError(response, err)
	}
	if err != nil {
		return 0, err
	}
	actual, err := g.Access(ctx, projectID, userID, true)
	if err != nil {
		return 0, err
	}
	if actual != level {
		return actual, Conflict("gitlab", "EFFECTIVE_PERMISSION_CONFLICT")
	}
	return actual, nil
}

// Credential reads the projected Secret on each operation. Kubernetes replaces
// its symlink atomically; configured files never fall back to stale env values.
func (g *GitLab) Credential() (string, error) {
	if g.TokenFile == "" {
		return g.Token, nil
	}
	raw, err := os.ReadFile(g.TokenFile)
	if err != nil || strings.TrimSpace(string(raw)) == "" {
		return "", &Error{Service: "gitlab", Code: "CREDENTIAL_UNAVAILABLE"}
	}
	return strings.TrimSpace(string(raw)), nil
}

type GitLabCredential struct {
	ID        int64    `json:"id"`
	UserID    int64    `json:"user_id"`
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
	Revoked   bool     `json:"revoked"`
}

func (g *GitLab) CredentialStatus(ctx context.Context) (GitLabCredential, error) {
	sdk, err := g.sdk()
	if err != nil {
		return GitLabCredential{}, err
	}
	token, response, err := sdk.PersonalAccessTokens.GetSinglePersonalAccessToken(gitlab.WithContext(ctx))
	if err != nil {
		return GitLabCredential{}, gitLabError(response, err)
	}
	if token == nil {
		return GitLabCredential{}, &Error{Service: "gitlab", Status: 502, Code: "INVALID_RESPONSE"}
	}
	status := GitLabCredential{
		ID:      token.ID,
		UserID:  token.UserID,
		Name:    token.Name,
		Scopes:  token.Scopes,
		Revoked: token.Revoked,
	}
	if token.ExpiresAt != nil {
		status.ExpiresAt = token.ExpiresAt.String()
	}
	return status, nil
}
