package tenantv1

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// DefaultSessionCookie is the browser session cookie forge-tenant issues.
const DefaultSessionCookie = "forge_session"

const identityUnavailableMessage = "identity service is temporarily unavailable"

// Identity resolves the incoming browser session or user API key against
// forge-tenant and injects the Principal into the request context. Requests
// without a valid identity never reach next.
func Identity(client *Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proof, err := RequestProof(r, DefaultSessionCookie)
			if err == nil {
				var principal *Principal
				if principal, err = client.ResolveIdentity(r.Context(), proof); err == nil {
					next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
					return
				}
			}
			WriteIdentityError(w, err)
		})
	}
}

// RequestProof builds the identity proof carried by an incoming request. It
// rejects a request that presents no credential or two of them. An empty
// Bearer credential counts as presented, so it never falls back to the cookie.
func RequestProof(r *http.Request, sessionCookie string) (*ResolveIdentityRequest, error) {
	session := ""
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		session = cookie.Value
	}
	token, hasBearer := bearerCredential(r.Header.Get("Authorization"))
	if hasBearer && session != "" {
		return nil, &Error{
			Status: http.StatusBadRequest, Code: "AMBIGUOUS_IDENTITY_PROOF",
			Message: "send either the session cookie or the bearer API key, not both",
		}
	}
	if hasBearer {
		if token == "" {
			return nil, &Error{
				Status: http.StatusUnauthorized, Code: "AUTH_REQUIRED",
				Message: "bearer API key is empty",
			}
		}
		return &ResolveIdentityRequest{Type: IdentityProofAPIKey, APIKey: token}, nil
	}
	if session == "" {
		return nil, &Error{
			Status: http.StatusUnauthorized, Code: "AUTH_REQUIRED",
			Message: "missing session cookie or bearer API key",
		}
	}
	return &ResolveIdentityRequest{
		Type: IdentityProofSession, Session: session,
		Method: r.Method, Origin: r.Header.Get("Origin"), CSRF: r.Header.Get("X-CSRF-Token"),
	}, nil
}

// IdentityFailure maps a resolution failure to the status and code the
// middleware answers with. Only the caller's own credential failures keep the
// tenant's 400, 401 or 403. Proxy and framework errors ("HTTPError"), a rejected
// service API key (SERVICE_AUTH_REQUIRED), other statuses and transport
// failures are a 503, so the end user is not told that a session expired.
func IdentityFailure(err error) (int, string) {
	var detail *Error
	if errors.As(err, &detail) && userCredentialFailure(detail) {
		return detail.Status, detail.Code
	}
	return http.StatusServiceUnavailable, "IDENTITY_UNAVAILABLE"
}

func userCredentialFailure(detail *Error) bool {
	switch detail.Status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
	default:
		return false
	}
	return detail.Code != "" && detail.Code != "HTTPError" && detail.Code != "SERVICE_AUTH_REQUIRED"
}

// IdentityErrorMessage is the client-safe message paired with IdentityFailure.
// Remapped infrastructure failures never echo the underlying error string.
func IdentityErrorMessage(err error) string {
	if _, code := IdentityFailure(err); code == "IDENTITY_UNAVAILABLE" {
		return identityUnavailableMessage
	}
	var detail *Error
	if errors.As(err, &detail) && detail.Message != "" {
		return detail.Message
	}
	return err.Error()
}

// WriteIdentityError writes the Forge error envelope for a failed resolution.
func WriteIdentityError(w http.ResponseWriter, err error) {
	status, code := IdentityFailure(err)
	message := IdentityErrorMessage(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(
		map[string]any{"error": map[string]any{"code": code, "message": message}},
	)
}

func bearerCredential(header string) (string, bool) {
	if header == "Bearer" {
		return "", true
	}
	return strings.CutPrefix(header, "Bearer ")
}
