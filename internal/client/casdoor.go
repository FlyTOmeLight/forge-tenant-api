package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
)

type Identity struct {
	ID          string `json:"id"`
	Owner       string `json:"owner"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Forbidden   bool   `json:"isForbidden"`
}

type Casdoor struct {
	URL, ClientID, Secret, Organization string
	HTTP                                *http.Client
}

func (c *Casdoor) Call(ctx context.Context, action string, query url.Values, body any, target any) error {
	method := "GET"
	if body != nil {
		method = "POST"
	}
	headers := http.Header{
		"Authorization": []string{
			"Basic " + base64.StdEncoding.EncodeToString([]byte(c.ClientID+":"+c.Secret)),
		},
	}
	var out struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := request(
		ctx,
		c.HTTP,
		"casdoor",
		method,
		c.URL+"/api/"+action+"?"+query.Encode(),
		body,
		headers,
		&out,
	); err != nil {
		return err
	}
	if out.Status != "ok" {
		return &Error{Service: "casdoor", Status: 422, Code: "REQUEST_REJECTED"}
	}
	if target != nil && len(out.Data) > 0 {
		return json.Unmarshal(out.Data, target)
	}
	return nil
}

func (c *Casdoor) User(ctx context.Context, name string) (*Identity, error) {
	var u *Identity
	err := c.Call(ctx, "get-user", url.Values{"id": {c.Organization + "/" + name}}, nil, &u)
	return u, err
}

func (c *Casdoor) CreateUser(ctx context.Context, name, display, email, password string) (*Identity, error) {
	u, err := c.User(ctx, name)
	if err != nil {
		return nil, err
	}
	// Names are generated from our persisted local user ID, never inferred from email.
	if u != nil {
		if u.Email != email || u.Owner != c.Organization {
			return nil, Conflict("casdoor", "IDENTITY_OWNERSHIP")
		}
		return u, nil
	}
	data := map[string]any{
		"owner":       c.Organization,
		"name":        name,
		"displayName": display,
		"email":       email,
		"password":    password,
		"type":        "normal-user",
		"isAdmin":     false,
		"isForbidden": false,
	}
	if err = c.Call(ctx, "add-user", nil, data, nil); err != nil {
		return nil, err
	}
	return c.User(ctx, name)
}
