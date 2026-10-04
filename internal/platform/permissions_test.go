package platform

import "testing"

func TestPlatformRoleDoesNotGrantTenantMembership(t *testing.T) {
	f := setup(t)
	code, out := f.call(t, f.operator, "POST", "/api/v1/platform/tenants", Object{"name": "Separate tenant", "slug": "separate", "admin_user_id": "member"}, 0, "")
	if code != 202 {
		t.Fatal(code, out)
	}
	tid := out["tenant_id"].(string)
	for _, test := range []struct {
		session, method, path string
		body                  Object
		want                  int
	}{
		{f.operator, "GET", "/api/v1/platform/tenants", nil, 200},
		{f.member, "GET", "/api/v1/platform/tenants", nil, 403},
		{f.member, "GET", "/api/v1/platform/connections", nil, 403},
		{f.operator, "GET", "/api/v1/tenants/" + tid, nil, 404},
		{f.operator, "POST", "/api/v1/tenants/" + tid + "/teams", Object{"name": "No bypass"}, 404},
		{f.member, "GET", "/api/v1/tenants/" + tid, nil, 200},
		{f.member, "PATCH", "/api/v1/platform/tenants/" + tid, Object{"status": "suspended"}, 403},
		{f.operator, "PATCH", "/api/v1/platform/tenants/" + tid, Object{"status": "suspended"}, 202},
	} {
		t.Run(test.method+test.path+test.session[:4], func(t *testing.T) {
			code, out := f.call(t, test.session, test.method, test.path, test.body, 1, "")
			if code != test.want {
				t.Fatal(code, out)
			}
		})
	}
	code, out = f.call(t, f.operator, "GET", "/api/v1/tenants", nil, 0, "")
	if code != 200 || len(out["items"].([]any)) != 0 {
		t.Fatal("platform operator acquired membership", code, out)
	}
	code, _ = f.call(t, f.operator, "POST", "/api/v1/platform/tenants", Object{"name": "Missing admin", "slug": "missing"}, 0, "")
	if code != 422 {
		t.Fatal("administrator must be explicit", code)
	}
}
func TestProjectPeopleAndAdministrativeCapabilities(t *testing.T) {
	f := setupTask(t)
	for _, test := range []struct {
		name                           string
		desired, observed, effective   int
		admin                          bool
		read, write, manage, configure bool
	}{
		{"reviewer", 20, 20, 20, false, true, false, false, false},
		{"developer", 30, 30, 30, false, true, true, false, false},
		{"project administrator", 40, 40, 40, false, true, true, true, true},
		{"unconfirmed administrator", 40, 20, 40, false, true, false, false, false},
		{"tenant administrator without code", 0, 0, 0, true, false, false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			role := "member"
			if test.admin {
				role = "admin"
			}
			if _, e := f.a.DB.Exec(f.ctx, `UPDATE memberships SET role=$1 WHERE id='mm'`, role); e != nil {
				t.Fatal(e)
			}
			if _, e := f.a.DB.Exec(f.ctx, `UPDATE grants SET desired_level=$1,observed_level=$2,effective_level=$3 WHERE membership_id='mm'`, test.desired, test.observed, test.effective); e != nil {
				t.Fatal(e)
			}
			code, out := f.call(t, f.member, "GET", "/api/v1/tenants/t", nil, 0, "")
			if code != 200 {
				t.Fatal(code, out)
			}
			p := out["projects"].([]any)[0].(map[string]any)["permissions"].(map[string]any)
			for k, want := range map[string]bool{"readCode": test.read, "createTask": test.write, "manageMembers": test.manage, "configureExecution": test.configure} {
				if p[k] != want {
					t.Fatal(k, p)
				}
			}
			code, out = f.call(t, f.member, "GET", "/api/v1/tenants/t/projects/p/people", nil, 0, "")
			if test.read {
				if code != 200 || len(out["items"].([]any)) != 2 {
					t.Fatal("project collaborators missing", code, out)
				}
			} else if code != 404 {
				t.Fatal("tenant administration leaked code membership", code, out)
			}
			code, _ = f.call(t, f.member, "GET", "/api/v1/tenants/t/projects/p/grant-candidates", nil, 0, "")
			if (code == 200) != test.manage {
				t.Fatal("grant policy differs from capabilities", code)
			}
		})
	}
}
