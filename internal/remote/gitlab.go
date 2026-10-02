package remote

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type GitLab struct {
	URL, Token string
	HTTP       *http.Client
}
type Group struct {
	ID          int64        `json:"id"`
	FullPath    string       `json:"full_path"`
	Description string       `json:"description"`
	Visibility  string       `json:"visibility"`
	Shared      []jsonObject `json:"shared_with_groups"`
}
type jsonObject map[string]any
type Project struct {
	ID          int64  `json:"id"`
	Path        string `json:"path_with_namespace"`
	Name        string `json:"name"`
	Visibility  string `json:"visibility"`
	Description string `json:"description"`
	Namespace   struct {
		ID int64 `json:"id"`
	} `json:"namespace"`
	Shared []jsonObject `json:"shared_with_groups"`
}
type GitUser struct {
	ID         int64  `json:"id"`
	Admin      bool   `json:"is_admin"`
	State      string `json:"state"`
	Identities []struct {
		Provider string `json:"provider"`
		UID      string `json:"extern_uid"`
	} `json:"identities"`
}

func (g *GitLab) call(ctx context.Context, method, path string, body, target any) error {
	return request(ctx, g.HTTP, "gitlab", method, g.URL+"/api/v4"+path, body, http.Header{"PRIVATE-TOKEN": []string{g.Token}}, target)
}
func (g *GitLab) Version(ctx context.Context) (map[string]any, error) {
	var v map[string]any
	err := g.call(ctx, "GET", "/version", nil, &v)
	return v, err
}
func (g *GitLab) EnsureGroup(ctx context.Context, id, slug, name string, remoteID int64) (int64, error) {
	marker := "managed-by:forge-tenant;tenant:" + id
	key := url.PathEscape(slug)
	if remoteID != 0 {
		key = strconv.FormatInt(remoteID, 10)
	}
	var group Group
	err := g.call(ctx, "GET", "/groups/"+key, nil, &group)
	if IsStatus(err, 404) && remoteID == 0 {
		err = g.call(ctx, "POST", "/groups", map[string]any{"name": name, "path": slug, "visibility": "private", "description": marker}, &group)
	}
	if err != nil {
		return 0, err
	}
	if group.Description != marker || group.FullPath != slug || group.Visibility != "private" || len(group.Shared) > 0 {
		return 0, Conflict("gitlab", "GROUP_OWNERSHIP_OR_VISIBILITY")
	}
	return group.ID, nil
}
func (g *GitLab) EnsureUser(ctx context.Context, subject, username, name, email string) (int64, error) {
	var users []GitUser
	if err := g.call(ctx, "GET", "/users?"+url.Values{"provider": {"openid_connect"}, "extern_uid": {subject}}.Encode(), nil, &users); err != nil {
		return 0, err
	}
	if len(users) > 1 {
		return 0, Conflict("gitlab", "DUPLICATE_EXTERNAL_IDENTITY")
	}
	var u GitUser
	if len(users) == 0 {
		err := g.call(ctx, "POST", "/users", map[string]any{"username": username, "name": name, "email": email, "force_random_password": true, "skip_confirmation": true, "provider": "openid_connect", "extern_uid": subject}, &u)
		if err != nil {
			return 0, err
		}
	} else {
		u = users[0]
	}
	// /users filtered responses may omit administrative fields: read the full user.
	if err := g.call(ctx, "GET", fmt.Sprintf("/users/%d", u.ID), nil, &u); err != nil {
		return 0, err
	}
	if u.Admin || u.State != "active" {
		return 0, Conflict("gitlab", "USER_ADMIN_OR_INACTIVE")
	}
	bound := false
	for _, identity := range u.Identities {
		if identity.Provider == "openid_connect" && identity.UID == subject {
			bound = true
		}
	}
	if !bound {
		return 0, Conflict("gitlab", "IDENTITY_BINDING_MISMATCH")
	}
	return u.ID, nil
}
func (g *GitLab) EnsureProject(ctx context.Context, id, path, name, mode string, groupID, remoteID int64) (int64, error) {
	marker := "managed-by:forge-tenant;project:" + id
	key := url.PathEscape(path)
	if remoteID != 0 {
		key = strconv.FormatInt(remoteID, 10)
	}
	var p Project
	err := g.call(ctx, "GET", "/projects/"+key, nil, &p)
	if IsStatus(err, 404) && remoteID == 0 && mode == "create" {
		parts := []rune(path)
		slash := 0
		for i, c := range parts {
			if c == '/' {
				slash = i + 1
			}
		}
		err = g.call(ctx, "POST", "/projects", map[string]any{"name": name, "path": string(parts[slash:]), "namespace_id": groupID, "visibility": "private", "description": marker, "initialize_with_readme": true}, &p)
	}
	if err != nil {
		return 0, err
	}
	if p.Namespace.ID != groupID || p.Path != path || p.Visibility != "private" || len(p.Shared) > 0 || (mode == "create" && p.Description != marker) {
		return 0, Conflict("gitlab", "PROJECT_SCOPE_OR_VISIBILITY")
	}
	return p.ID, nil
}
func (g *GitLab) Access(ctx context.Context, projectID, userID int64, all bool) (int, error) {
	middle := ""
	if all {
		middle = "/all"
	}
	var m struct {
		Level int `json:"access_level"`
	}
	err := g.call(ctx, "GET", fmt.Sprintf("/projects/%d/members%s/%d", projectID, middle, userID), nil, &m)
	if IsStatus(err, 404) {
		return 0, nil
	}
	return m.Level, err
}
func (g *GitLab) SetAccess(ctx context.Context, projectID, userID int64, level int) (int, error) {
	direct, err := g.Access(ctx, projectID, userID, false)
	if err != nil {
		return 0, err
	}
	path := fmt.Sprintf("/projects/%d/members", projectID)
	switch {
	case level == 0 && direct > 0:
		err = g.call(ctx, "DELETE", path+"/"+strconv.FormatInt(userID, 10), nil, nil)
		if IsStatus(err, 404) {
			err = nil
		}
	case level > 0 && direct == 0:
		err = g.call(ctx, "POST", path, map[string]any{"user_id": userID, "access_level": level}, nil)
	case level > 0 && direct != level:
		err = g.call(ctx, "PUT", path+"/"+strconv.FormatInt(userID, 10), map[string]any{"access_level": level}, nil)
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
