package tenant

import "testing"

func TestPlatformRoleDoesNotGrantTenantMembership(t *testing.T) {
	f := setup(t)
	code, out := f.call(
		t,
		f.operator,
		"POST",
		"/api/v1/platform/tenants",
		Object{"name": "Separate tenant", "slug": "separate", "admin_user_id": "member"},
		0,
		"",
	)
	if code != 202 {
		t.Fatal(code, out)
	}
	tid := out["tenant_id"].(string)
	for _, test := range []struct {
		session, method, path string
		body                  Object
		want                  int
	}{
		{session: f.operator, method: "GET", path: "/api/v1/platform/tenants", body: nil, want: 200},
		{session: f.member, method: "GET", path: "/api/v1/platform/tenants", body: nil, want: 403},
		{session: f.operator, method: "GET", path: "/api/v1/tenants/" + tid, body: nil, want: 404},
		{session: f.operator, method: "POST", path: "/api/v1/tenants/" + tid + "/teams", body: Object{"name": "No bypass"}, want: 404},
		{session: f.member, method: "GET", path: "/api/v1/tenants/" + tid, body: nil, want: 200},
		{session: f.member, method: "PATCH", path: "/api/v1/platform/tenants/" + tid, body: Object{"status": "suspended"}, want: 403},
		{session: f.operator, method: "PATCH", path: "/api/v1/platform/tenants/" + tid, body: Object{"status": "suspended"}, want: 202},
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
	code, _ = f.call(
		t,
		f.operator,
		"POST",
		"/api/v1/platform/tenants",
		Object{"name": "Missing admin", "slug": "missing"},
		0,
		"",
	)
	if code != 422 {
		t.Fatal("administrator must be explicit", code)
	}
}

func TestProjectPeopleAndAdministrativeCapabilities(t *testing.T) {
	f := setupProject(t)
	for _, test := range []struct {
		name                           string
		desired, observed, effective   int
		admin                          bool
		read, write, manage, configure bool
	}{
		{name: "reviewer", desired: 20, observed: 20, effective: 20, admin: false, read: true, write: false, manage: false, configure: false},
		{name: "developer", desired: 30, observed: 30, effective: 30, admin: false, read: true, write: true, manage: false, configure: false},
		{name: "project administrator", desired: 40, observed: 40, effective: 40, admin: false, read: true, write: true, manage: true, configure: true},
		{name: "unconfirmed administrator", desired: 40, observed: 20, effective: 40, admin: false, read: true, write: false, manage: false, configure: false},
		{name: "tenant administrator without code", desired: 0, observed: 0, effective: 0, admin: true, read: false, write: false, manage: true, configure: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			role := "member"
			if test.admin {
				role = "admin"
			}
			if _, e := f.a.DB.Exec(f.ctx, `UPDATE memberships SET role=$1 WHERE id='mm'`, role); e != nil {
				t.Fatal(e)
			}
			if _, e := f.a.DB.Exec(
				f.ctx,
				`UPDATE grants SET desired_level=$1,observed_level=$2,effective_level=$3 WHERE membership_id='mm'`,
				test.desired,
				test.observed,
				test.effective,
			); e != nil {
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
			code, _ = f.call(
				t,
				f.member,
				"GET",
				"/api/v1/tenants/t/projects/p/grant-candidates",
				nil,
				0,
				"",
			)
			if (code == 200) != test.manage {
				t.Fatal("grant policy differs from capabilities", code)
			}
		})
	}
}
