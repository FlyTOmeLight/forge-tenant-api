package tenant

import (
	"bytes"
	"io"
	"net/http"
)

func (a *Controller) mutate(action string, fn mutation) func(http.ResponseWriter, *http.Request, User) {
	return func(w http.ResponseWriter, r *http.Request, u User) {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
		if err != nil {
			fail(w, problem(400, "INVALID_BODY", "请求过大"))
			return
		}
		body := Object{}
		if len(raw) > 0 {
			r.Body = io.NopCloser(bytes.NewReader(raw))
			if err = readJSON(w, r, &body); err != nil {
				fail(w, problem(400, "INVALID_JSON", "请求格式无效"))
				return
			}
		}
		change := Change{
			TenantID:    r.PathValue("tid"),
			ProjectID:   r.PathValue("pid"),
			MemberID:    r.PathValue("mid"),
			TeamID:      r.PathValue("team"),
			OperationID: r.PathValue("oid"),
			Remove:      r.Method == "DELETE",
			Key:         r.Header.Get("Idempotency-Key"),
			Version:     r.Header.Get("If-Match"),
			Route:       r.Method + " " + r.URL.Path,
			Target:      r.URL.Path,
			Raw:         raw,
		}
		result, err := a.service.change(r.Context(), u, action, fn, change, body)
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(result.Status)
		_, _ = w.Write(result.Data)
	}
}
