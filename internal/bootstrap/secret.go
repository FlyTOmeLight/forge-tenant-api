package bootstrap

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
)

const leaseKey = "forge.io/bootstrap-lease"
const leaseUntilKey = "forge.io/bootstrap-lease-until"

type SecretState struct {
	Metadata struct {
		ResourceVersion string            `json:"resourceVersion"`
		Annotations     map[string]string `json:"annotations"`
	} `json:"metadata"`
	Data map[string]string `json:"data"`
}

// SecretStore only addresses the single pre-created output Secret. It requires
// get/patch, never create/list/update/delete permission.
type SecretStore struct {
	HTTP           *http.Client
	URL, TokenFile string
	holder         string
}

func InClusterStore(namespace, name string) (*SecretStore, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, errors.New("Kubernetes service environment is required")
	}
	transport, err := client.NewHTTP("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return nil, err
	}
	return &SecretStore{HTTP: transport, URL: "https://" + host + ":" + port + "/api/v1/namespaces/" + url.PathEscape(namespace) + "/secrets/" + url.PathEscape(name), TokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token"}, nil
}

func (s *SecretStore) request(ctx context.Context, method string, body any) (*SecretState, error) {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, s.URL, bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("invalid output Secret endpoint")
	}
	token, err := os.ReadFile(s.TokenFile)
	if err != nil {
		return nil, errors.New("cannot read Kubernetes service account token")
	}
	req.Header.Set("Authorization", "Bearer "+string(bytes.TrimSpace(token)))
	if method == http.MethodPatch {
		req.Header.Set("Content-Type", "application/merge-patch+json")
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("output Secret request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("output Secret %s failed (HTTP %d)", method, resp.StatusCode)
	}
	var result SecretState
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return nil, errors.New("invalid output Secret response")
	}
	if result.Data == nil {
		result.Data = map[string]string{}
	}
	for key, value := range result.Data {
		raw, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, errors.New("invalid output Secret encoding")
		}
		result.Data[key] = string(raw)
	}
	return &result, nil
}

func (s *SecretStore) Acquire(ctx context.Context, duration time.Duration) (*SecretState, error) {
	state, err := s.request(ctx, http.MethodGet, nil)
	if err != nil {
		return nil, err
	}
	if state.Metadata.Annotations[leaseKey] != "" {
		until, err := time.Parse(time.RFC3339, state.Metadata.Annotations[leaseUntilKey])
		if err != nil || time.Now().Before(until) {
			return nil, errors.New("external bootstrap is already running; retry after its bounded lease")
		}
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	s.holder = hex.EncodeToString(random)
	body := map[string]any{"metadata": map[string]any{"resourceVersion": state.Metadata.ResourceVersion, "annotations": map[string]string{leaseKey: s.holder, leaseUntilKey: time.Now().Add(duration).UTC().Format(time.RFC3339)}}}
	result, err := s.request(ctx, http.MethodPatch, body)
	if err != nil {
		// A response can be lost after the compare-and-swap succeeded.
		result, err = s.request(ctx, http.MethodGet, nil)
		if err != nil || result.Metadata.Annotations[leaseKey] != s.holder {
			return nil, errors.New("cannot acquire external bootstrap lease")
		}
	}
	return result, nil
}

func (s *SecretStore) Save(ctx context.Context, state *SecretState) error {
	encoded := map[string]string{}
	for key, value := range state.Data {
		encoded[key] = base64.StdEncoding.EncodeToString([]byte(value))
	}
	result, err := s.request(ctx, http.MethodPatch, map[string]any{"metadata": map[string]any{"resourceVersion": state.Metadata.ResourceVersion}, "data": encoded})
	if err != nil {
		return err
	}
	*state = *result
	return nil
}

func (s *SecretStore) Release(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	state, err := s.request(ctx, http.MethodGet, nil)
	if err != nil || state.Metadata.Annotations[leaseKey] != s.holder {
		return
	}
	_, _ = s.request(ctx, http.MethodPatch, map[string]any{"metadata": map[string]any{"resourceVersion": state.Metadata.ResourceVersion, "annotations": map[string]any{leaseKey: nil, leaseUntilKey: nil}}})
}
