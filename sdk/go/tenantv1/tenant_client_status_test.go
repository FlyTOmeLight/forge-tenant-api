package tenantv1

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetTenantStatus(t *testing.T) {
	for _, tc := range []struct {
		name, response, wantStatus, wantErr string
	}{
		{name: "active", response: `{"tenant_id":"tenant-a","status":"active"}`, wantStatus: "active"},
		{name: "suspended", response: `{"tenant_id":"tenant-a","status":"suspended"}`, wantStatus: "suspended"},
		{name: "wrong tenant", response: `{"tenant_id":"tenant-b","status":"active"}`, wantErr: "tenant API returned tenant"},
		{name: "unknown state", response: `{"tenant_id":"tenant-a","status":"deleted"}`, wantErr: "unknown tenant status"},
		{name: "empty state", response: `{"tenant_id":"tenant-a"}`, wantErr: "unknown tenant status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/internal/v1/tenants/tenant-a/status" {
					t.Errorf("request %s %s", r.Method, r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("s", 32) {
					t.Error("missing service bearer")
				}
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			status, err := newTestClient(t, server.URL).GetTenantStatus(t.Context(), "tenant-a")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if status.TenantID != "tenant-a" || status.Status != tc.wantStatus {
				t.Fatalf("status = %#v", status)
			}
		})
	}
}

func TestGetTenantStatusRejectsMissingTenant(t *testing.T) {
	client := newTestClient(t, "https://example.test")
	if _, err := client.GetTenantStatus(t.Context(), ""); err == nil {
		t.Fatal("empty tenant ID accepted")
	}
	if _, err := client.GetTenantStatus(t.Context(), "tenant/other"); err == nil {
		t.Fatal("tenant ID containing a path separator accepted")
	}
}

func TestGetTenantStatusMapsNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"TENANT_NOT_FOUND","message":"missing"}}`))
	}))
	defer server.Close()
	_, err := newTestClient(t, server.URL).GetTenantStatus(t.Context(), "tenant-a")
	var detail *Error
	if !errors.As(err, &detail) || detail.Status != http.StatusNotFound || detail.Code != "TENANT_NOT_FOUND" {
		t.Fatalf("error = %#v", err)
	}
}
