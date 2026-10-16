package bucketprovision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client speaks the storage management wire contract without importing artifact's module.
// Its service credential is never stored in task payloads or returned errors.
type Client struct {
	URL, Token string
	HTTP       *http.Client
	Timeout    time.Duration
}

type Bucket struct {
	TenantID  string `json:"tenantID"`
	BackendID string `json:"backendID"`
	Bucket    string `json:"bucket"`
}

func (c *Client) Ensure(ctx context.Context, tenantID string) (Bucket, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	body, _ := json.Marshal(struct {
		TenantID string `json:"tenantID"`
	}{TenantID: tenantID})
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.URL+"/v1/storage/tenant-buckets",
		bytes.NewReader(body),
	)
	if err != nil {
		return Bucket{}, errors.New("invalid storage request")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return Bucket{}, errors.New("storage request unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return Bucket{}, fmt.Errorf("storage returned HTTP %d", response.StatusCode)
	}
	var result Bucket
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64*1024))
	if err = decoder.Decode(&result); err != nil {
		return Bucket{}, errors.New("invalid storage response")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || result.TenantID != tenantID || result.BackendID == "" ||
		result.Bucket == "" {
		return Bucket{}, errors.New("storage response identity mismatch")
	}
	return result, nil
}
