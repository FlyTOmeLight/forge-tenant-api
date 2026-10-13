package tenant

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWorkspaceMembershipDoesNotRequireRepositoryGrant(t *testing.T) {
	f := setupProject(t)
	call := func(uid, tid, token string, want int) map[string]any {
		t.Helper()
		b, _ := json.Marshal(Object{"user_id": uid, "tenant_id": tid})
		r := httptest.NewRequest("POST", "/internal/v1/workspace/check", bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		apiHandler(t, f.a).ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("want %d got %d: %s", want, w.Code, w.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	call("member", "t", "", 401)
	out := call("member", "t", f.a.Config.ServiceToken, 200)
	if len(out["projects"].([]any)) != 1 {
		t.Fatal(out)
	}
	if err := f.a.DB.WithContext(f.ctx).
		Exec("UPDATE grants SET effective_level=0 WHERE membership_id='mm'").
		Error; err != nil {
		t.Fatal(err)
	}
	out = call("member", "t", f.a.Config.ServiceToken, 200)
	if len(out["projects"].([]any)) != 0 || out["canEdit"] != true {
		t.Fatal(out)
	}
	call("member", "foreign", f.a.Config.ServiceToken, 403)
	if err := f.a.DB.WithContext(f.ctx).
		Exec("UPDATE memberships SET status='removed' WHERE id='mm'").
		Error; err != nil {
		t.Fatal(err)
	}
	call("member", "t", f.a.Config.ServiceToken, 403)
}
