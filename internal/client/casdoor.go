package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
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

func (c *Casdoor) sdk() *casdoorsdk.Client {
	return casdoorsdk.NewClient(c.URL, c.ClientID, c.Secret, "", c.Organization, "")
}

// Casdoor SDK v1.56.0 only exposes a package-global HTTP client and its user
// methods do not accept context. Keep transport local to preserve cancellation
// and the configured CA; use the SDK's URLs, response and user types.
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
	parameters := make(map[string]string, len(query))
	for key := range query {
		parameters[key] = query.Get(key)
	}
	var out casdoorsdk.Response
	if err := request(
		ctx,
		c.HTTP,
		"casdoor",
		method,
		c.sdk().GetUrl(action, parameters),
		body,
		headers,
		&out,
	); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if out.Status != "ok" {
		return &Error{Service: "casdoor", Status: 422, Code: "REQUEST_REJECTED"}
	}
	if target != nil {
		data, err := json.Marshal(out.Data)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, target); err != nil {
			return &Error{Service: "casdoor", Status: 502, Code: "INVALID_RESPONSE"}
		}
	}
	return nil
}

func (c *Casdoor) User(ctx context.Context, name string) (*Identity, error) {
	var u *casdoorsdk.User
	err := c.Call(ctx, "get-user", url.Values{"id": {c.sdk().GetId(name)}}, nil, &u)
	if err != nil || u == nil {
		return nil, err
	}
	return &Identity{
		ID:          u.Id,
		Owner:       u.Owner,
		Name:        u.Name,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		Forbidden:   u.IsForbidden,
	}, nil
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
	data := &casdoorsdk.User{
		Owner:       c.Organization,
		Name:        name,
		DisplayName: display,
		Email:       email,
		Password:    password,
		Type:        "normal-user",
	}
	if err = c.Call(ctx, "add-user", nil, data, nil); err != nil {
		return nil, err
	}
	return c.User(ctx, name)
}
