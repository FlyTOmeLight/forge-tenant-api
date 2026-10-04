package platform

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type taskFixture struct {
	*fixture
	task          CodingTask
	mrState, head string
}

func setupTask(t *testing.T) *taskFixture {
	f := &taskFixture{fixture: setup(t), mrState: "opened", head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	_, e := f.a.DB.Exec(f.ctx, `INSERT INTO tenants(id,name,slug,status) VALUES('t','Tenant','task-test','active'); INSERT INTO projects(id,tenant_id,name,path,mode,gitlab_id) VALUES('p','t','Repo','task-test/repo','bind',77); INSERT INTO memberships(id,tenant_id,user_id,role,status) VALUES('ma','t','operator','admin','active'),('mm','t','member','member','active'); INSERT INTO grants(tenant_id,project_id,membership_id,desired_level,observed_level,effective_level) VALUES('t','p','ma',40,40,40),('t','p','mm',30,30,30)`)
	if e != nil {
		t.Fatal(e)
	}
	if _, err := f.a.DB.Exec(f.ctx, `INSERT INTO direct_grants SELECT tenant_id,project_id,membership_id,desired_level FROM grants WHERE desired_level>0 ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	config := CodingSettings{Version: 1, BaseBranch: "master", Model: "local-model", AllowedPaths: []string{"src/"}, Checks: []string{"go test ./..."}, RepairLimit: 3, TimeoutMinutes: 30}
	rawConfig, _ := json.Marshal(config)
	if _, e = f.a.DB.Exec(f.ctx, `INSERT INTO coding_project_settings(tenant_id,project_id,version,settings,checked_at) VALUES('t','p',1,$1,now())`, rawConfig); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/repository/branches/") {
			JSON(w, 200, Object{"name": "master", "commit": Object{"id": f.head}})
			return
		}
		if r.URL.Path == "/api/v4/projects/77/merge_requests/1/discussions" {
			_ = json.NewEncoder(w).Encode([]any{Object{"id": "discussion", "notes": []any{Object{"body": "nested traversal", "position": Object{"new_path": "path.go", "new_line": 69, "head_sha": f.head}}}}})
			return
		}
		JSON(w, 200, Object{"iid": 1, "state": f.mrState, "sha": f.head, "source_branch": "fix", "target_branch": "master"})
	}))
	t.Cleanup(server.Close)
	f.a.GitLab.URL = server.URL
	code, out := f.call(t, f.operator, "POST", "/api/v1/tenants/t/projects/p/tasks", Object{"title": "Fix traversal", "description": "a/../b passes", "criteria": "reject raw ..", "mrIid": 1, "synthetic": true}, 0, "")
	if code != 201 {
		t.Fatal(code, out)
	}
	raw, _ := json.Marshal(out)
	if e = json.Unmarshal(raw, &f.task); e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *taskFixture) act(t *testing.T, action string, extra Object) (int, Object) {
	t.Helper()
	body := Object{"action": action, "version": f.task.Version, "head": f.task.Head, "text": "Fix raw traversal"}
	for k, v := range extra {
		body[k] = v
	}
	code, out := f.call(t, f.operator, "POST", "/api/v1/tenants/t/projects/p/tasks/"+f.task.ID+"/actions", body, 0, "")
	if code == 200 {
		raw, _ := json.Marshal(out)
		_ = json.Unmarshal(raw, &f.task)
	}
	return code, out
}
func (f *taskFixture) refresh(t *testing.T) {
	t.Helper()
	code, out := f.call(t, f.operator, "GET", "/api/v1/tenants/t/projects/p/tasks/"+f.task.ID, nil, 0, "")
	if code != 200 {
		t.Fatal(code, out)
	}
	raw, _ := json.Marshal(out["task"])
	_ = json.Unmarshal(raw, &f.task)
}
func (f *taskFixture) internal(t *testing.T, path string, b Object) (int, Object) {
	t.Helper()
	r := httptest.NewRequest("POST", path, func() *bytes.Reader { raw, _ := json.Marshal(b); return bytes.NewReader(raw) }())
	r.Header.Set("Authorization", "Bearer "+f.a.Config.ServiceToken)
	w := httptest.NewRecorder()
	f.a.Handler().ServeHTTP(w, r)
	out := Object{}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(w.Body.String())
	}
	return w.Code, out
}
func TestTasksHumanControlFencesLateDelivery(t *testing.T) {
	f := setupTask(t)
	code, out := f.act(t, "Resume", nil)
	if code != 200 {
		t.Fatal(code, out)
	}
	code, out = f.internal(t, "/internal/v1/tasks/claim", Object{"worker": "executor"})
	if code != 200 {
		t.Fatal(code, out)
	}
	run := out["run"].(map[string]any)
	id := run["id"].(string)
	epoch := run["epoch"]
	f.refresh(t)
	body := Object{"runId": id, "epoch": epoch, "worker": "executor"}
	code, _ = f.internal(t, "/internal/v1/tasks/"+f.task.ID+"/gate", body)
	if code != 200 {
		t.Fatal(code)
	}
	code, out = f.act(t, "Intervene", nil)
	if code != 200 || f.task.Control != "Stopping" || f.task.StopConfirmed {
		t.Fatal(code, out)
	}
	code, _ = f.internal(t, "/internal/v1/tasks/"+f.task.ID+"/gate", body)
	if code != 409 {
		t.Fatal("late write permitted", code)
	}
	code, _ = f.act(t, "Resume", nil)
	if code != 409 {
		t.Fatal("resume before stop", code)
	}
	body["status"] = "Passed"
	body["head"] = f.head
	code, _ = f.internal(t, "/internal/v1/tasks/"+f.task.ID+"/finish", body)
	if code != 409 {
		t.Fatal("late delivery accepted", code)
	}
	body["status"] = "Stopped"
	code, out = f.internal(t, "/internal/v1/tasks/"+f.task.ID+"/finish", body)
	if code != 200 {
		t.Fatal(code, out)
	}
	f.refresh(t)
	if f.task.Control != "Human" || !f.task.StopConfirmed || f.task.ActiveRun != nil {
		t.Fatal(f.task)
	}
	code, _ = f.act(t, "Feedback", nil)
	if code != 200 || f.task.Control != "Human" || f.task.ActiveRun != nil {
		t.Fatal("feedback stole control", code)
	}
	code, out = f.act(t, "Resume", nil)
	if code != 200 {
		t.Fatal(code, out)
	}
}
func TestTaskApprovalAndMRFacts(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "approval_is_not_completion", true: "cancel_is_retained_after_merge"}[cancel], func(t *testing.T) {
			f := setupTask(t)
			code, out := f.act(t, "Approve", nil)
			if code != 200 || f.task.Status != "Review" {
				t.Fatal(code, out)
			}
			if cancel {
				code, out = f.act(t, "Cancel", nil)
				if code != 200 {
					t.Fatal(code, out)
				}
			}
			f.mrState = "merged"
			code, out = f.call(t, f.operator, "POST", "/api/v1/tenants/t/projects/p/tasks/"+f.task.ID+"/sync", Object{}, 0, "")
			if code != 200 {
				t.Fatal(code, out)
			}
			f.refresh(t)
			want := "Completed"
			if cancel {
				want = "Canceled"
			}
			if f.task.Status != want {
				t.Fatal(f.task.Status, want)
			}
		})
	}
}
func TestTaskTransferAndCodeVersion(t *testing.T) {
	f := setupTask(t)
	original := f.task
	code, out := f.act(t, "Transfer", Object{"ownerId": "member"})
	if code != 200 {
		t.Fatal(code, out)
	}
	if f.task.OwnerID != "member" || f.task.Head != original.Head || f.task.Control != original.Control || f.task.MRIID != original.MRIID {
		t.Fatal(f.task)
	}
	code, _ = f.act(t, "Approve", nil)
	if code != 200 {
		t.Fatal(code)
	}
	f.head = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	code, out = f.call(t, f.operator, "POST", "/api/v1/tenants/t/projects/p/tasks/"+f.task.ID+"/sync", Object{}, 0, "")
	if code != 200 {
		t.Fatal(code, out)
	}
	f.refresh(t)
	if f.task.Head != f.head || f.task.ApprovalHead != "" || f.task.Checks != "NotRun" || f.task.Control != "Human" {
		t.Fatal(f.task)
	}
	code, _ = f.act(t, "Approve", Object{"head": original.Head})
	if code != 409 {
		t.Fatal("stale approval accepted", code)
	}
}
func TestTaskReadPermissionsAndRefreshPersistence(t *testing.T) {
	f := setupTask(t)
	path := "/api/v1/tenants/t/projects/p/tasks/" + f.task.ID
	code, _ := f.call(t, f.other, "GET", path, nil, 0, "")
	if code != 404 {
		t.Fatal("other user read code", code)
	}
	_, e := f.a.DB.Exec(f.ctx, `UPDATE grants SET desired_level=0 WHERE membership_id='ma'`)
	if e != nil {
		t.Fatal(e)
	}
	code, _ = f.call(t, f.operator, "GET", path, nil, 0, "")
	if code != 404 {
		t.Fatal("operator bypassed code grant", code)
	}
	code, out := f.call(t, f.member, "GET", path, nil, 0, "")
	if code != 200 || out["task"].(map[string]any)["id"] != f.task.ID {
		t.Fatal(code, out)
	}
}
func TestExpiredTaskLeaseDoesNotConfirmStop(t *testing.T) {
	f := setupTask(t)
	code, _ := f.act(t, "Resume", nil)
	if code != 200 {
		t.Fatal(code)
	}
	code, out := f.internal(t, "/internal/v1/tasks/claim", Object{"worker": "executor"})
	if code != 200 {
		t.Fatal(code, out)
	}
	run := out["run"].(map[string]any)
	_, e := f.a.DB.Exec(f.ctx, `UPDATE coding_runs SET lease_until=$2 WHERE id=$1`, run["id"], time.Now().Add(-time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	if e = f.a.expireTaskLeases(f.ctx); e != nil {
		t.Fatal(e)
	}
	f.refresh(t)
	if f.task.Control != "Stopping" || f.task.StopConfirmed {
		t.Fatal(f.task)
	}
	code, _ = f.act(t, "Resume", nil)
	if code != 409 {
		t.Fatal(code)
	}
}
func TestQueuedTaskInterventionNeedsNoWorkerStop(t *testing.T) {
	f := setupTask(t)
	code, out := f.act(t, "Resume", nil)
	if code != 200 {
		t.Fatal(code, out)
	}
	code, out = f.act(t, "Intervene", nil)
	if code != 200 || !f.task.StopConfirmed || f.task.Control != "Human" || f.task.ActiveRun != nil {
		t.Fatal(code, out)
	}
	code, out = f.act(t, "Resume", nil)
	if code != 200 {
		t.Fatal(code, out)
	}
}
func TestTaskFeedbackQueueRequiresNewCommitConfirmation(t *testing.T) {
	f := setupTask(t)
	code, out := f.act(t, "Resume", nil)
	if code != 200 {
		t.Fatal(code, out)
	}
	code, out = f.internal(t, "/internal/v1/tasks/claim", Object{"worker": "executor"})
	if code != 200 {
		t.Fatal(code, out)
	}
	run := out["run"].(map[string]any)
	f.refresh(t)
	code, out = f.act(t, "Feedback", Object{"text": "Add nested traversal regression"})
	if code != 200 {
		t.Fatal(code, out)
	}
	if f.task.Round != 1 {
		t.Fatal("queued feedback started parallel round")
	}
	f.head = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	code, out = f.internal(t, "/internal/v1/tasks/"+f.task.ID+"/finish", Object{"runId": run["id"], "epoch": run["epoch"], "worker": "executor", "status": "Passed", "head": f.head})
	if code != 200 {
		t.Fatal(code, out)
	}
	f.refresh(t)
	code, _ = f.act(t, "NextFeedback", nil)
	if code != 409 {
		t.Fatal("old feedback ran against new code", code)
	}
	code, out = f.call(t, f.operator, "GET", "/api/v1/tenants/t/projects/p/tasks/"+f.task.ID, nil, 0, "")
	if code != 200 {
		t.Fatal(code, out)
	}
	feedback := out["feedback"].([]any)[0].(map[string]any)
	code, out = f.act(t, "ReconfirmFeedback", Object{"feedbackId": feedback["id"]})
	if code != 200 {
		t.Fatal(code, out)
	}
	code, out = f.act(t, "NextFeedback", nil)
	if code != 200 || f.task.Round != 2 {
		t.Fatal(code, out)
	}
}
func TestTaskGrantReenableDoesNotReviveOldExecutor(t *testing.T) {
	f := setupTask(t)
	code, _ := f.act(t, "Resume", nil)
	if code != 200 {
		t.Fatal(code)
	}
	code, out := f.internal(t, "/internal/v1/tasks/claim", Object{"worker": "executor"})
	if code != 200 {
		t.Fatal(code, out)
	}
	run := out["run"].(map[string]any)
	_, e := f.a.DB.Exec(f.ctx, `UPDATE grants SET desired_level=0 WHERE membership_id='ma'`)
	if e != nil {
		t.Fatal(e)
	}
	body := Object{"runId": run["id"], "epoch": run["epoch"], "worker": "executor"}
	code, _ = f.internal(t, "/internal/v1/tasks/"+f.task.ID+"/gate", body)
	if code != 409 {
		t.Fatal(code)
	}
	_, e = f.a.DB.Exec(f.ctx, `UPDATE grants SET desired_level=40 WHERE membership_id='ma'`)
	if e != nil {
		t.Fatal(e)
	}
	code, _ = f.internal(t, "/internal/v1/tasks/"+f.task.ID+"/gate", body)
	if code != 409 {
		t.Fatal("regrant revived old epoch", code)
	}
}

func TestTaskExecutorClaimsOnlyConfiguredProject(t *testing.T) {
	f := setupTask(t)
	if code, out := f.act(t, "Resume", nil); code != 200 {
		t.Fatal(code, out)
	}
	if code, out := f.internal(t, "/internal/v1/tasks/claim", Object{"worker": "other-project", "gitlabProjectId": 88}); code != 200 || out["run"] != nil {
		t.Fatal(code, out)
	}
	if code, out := f.internal(t, "/internal/v1/tasks/claim", Object{"worker": "configured-project", "gitlabProjectId": 77}); code != 200 || out["run"] == nil {
		t.Fatal(code, out)
	}
}

func TestProblemCreationQueuesWithoutEmptyMRAndReplays(t *testing.T) {
	f := setupTask(t)
	payload := Object{"title": "Fix new issue", "description": "bad request", "reproduce": "send empty parameter", "criteria": "no error", "constraints": "keep interfaces"}
	path := "/api/v1/tenants/t/projects/p/tasks/new"
	code, out := f.call(t, f.operator, "POST", path, payload, 0, "new-task-key")
	if code != 201 {
		t.Fatal(code, out)
	}
	id := out["id"]
	if out["mrIid"] != float64(0) || out["status"] != "Queued" || out["control"] != "Agent" {
		t.Fatal(out)
	}
	if code, replay := f.call(t, f.operator, "POST", path, payload, 0, "new-task-key"); code != 200 || replay["id"] != id {
		t.Fatal(code, replay)
	}
	payload["title"] = "different request"
	if code, out := f.call(t, f.operator, "POST", path, payload, 0, "new-task-key"); code != 409 {
		t.Fatal(code, out)
	}
	code, claimed := f.internal(t, "/internal/v1/tasks/claim", Object{"worker": "generic", "gitlabProjectId": 77})
	if code != 200 {
		t.Fatal(code, claimed)
	}
	run := claimed["run"].(map[string]any)
	if run["projectPath"] != "task-test/repo" || run["config"].(map[string]any)["model"] != "local-model" {
		t.Fatal(run)
	}
	// A settings edit must not alter an already queued round.
	if _, e := f.a.DB.Exec(f.ctx, `UPDATE coding_project_settings SET version=2,settings=jsonb_set(settings,'{model}','"new-model"') WHERE project_id='p'`); e != nil {
		t.Fatal(e)
	}
	var model string
	if e := f.a.DB.QueryRow(f.ctx, `SELECT config_snapshot->>'model' FROM coding_runs WHERE id=$1`, run["id"]).Scan(&model); e != nil || model != "local-model" {
		t.Fatal(model, e)
	}
}
func TestProjectConfigChangesRequireFreshCheck(t *testing.T) {
	f := setupTask(t)
	body := Object{"version": 1, "baseBranch": "master", "model": "local-model", "allowedPaths": []string{"src/"}, "checks": []string{"go test ./..."}, "repairLimit": 3, "timeoutMinutes": 30}
	code, out := f.call(t, f.operator, "PUT", "/api/v1/tenants/t/projects/p/coding-settings", body, 0, "")
	if code != 200 || out["version"] != float64(2) || out["checkedAt"] != nil {
		t.Fatal(code, out)
	}
	if code, out := f.act(t, "Resume", nil); code != 409 {
		t.Fatal(code, out)
	}
	if code, out := f.call(t, f.operator, "PUT", "/api/v1/tenants/t/projects/p/coding-settings", body, 0, ""); code != 412 {
		t.Fatal(code, out)
	}
}
func TestReadOnlyDesignatedReviewerCanApproveButNotIntervene(t *testing.T) {
	f := setupTask(t)
	if _, e := f.a.DB.Exec(f.ctx, `UPDATE grants SET desired_level=20,observed_level=20,effective_level=20 WHERE membership_id='ma'`); e != nil {
		t.Fatal(e)
	}
	if code, out := f.act(t, "Approve", nil); code != 200 {
		t.Fatal(code, out)
	}
	if code, out := f.act(t, "Intervene", nil); code != 403 {
		t.Fatal(code, out)
	}
	code, out := f.call(t, f.operator, "GET", "/api/v1/tenants/t/projects/p/tasks/"+f.task.ID, nil, 0, "")
	if code != 200 || out["permissions"].(map[string]any)["manage"] != false {
		t.Fatal(code, out)
	}
}

// Exercise an actual v3 task row through repeated upgrades, not just empty schemas.
func TestTaskSchemaUpgradePreservesAssociatedMR(t *testing.T) {
	f := setupTask(t)
	legacy, err := os.ReadFile("testdata/tasks-v3.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.a.DB.Exec(f.ctx, `DROP TABLE coding_events,coding_feedback,coding_runs,coding_tasks,coding_project_settings,coding_executors; DELETE FROM schema_versions WHERE version BETWEEN 3 AND 4`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.a.DB.Exec(f.ctx, string(legacy)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.a.DB.Exec(f.ctx, `INSERT INTO coding_tasks(id,tenant_id,project_id,owner_id,title,description,criteria,branch,target_branch,mr_iid,head,reviewer_ids) VALUES('legacy','t','p','operator','Existing task','Reproduce','Criteria','fix','master',1,$1,ARRAY['operator'])`, f.head); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = f.a.Migrate(f.ctx); err != nil {
			t.Fatal(err)
		}
	}
	task, err := scanTask(f.a.DB.QueryRow(f.ctx, `SELECT `+taskColumns+` FROM coding_tasks WHERE id='legacy'`))
	if err != nil {
		t.Fatal(err)
	}
	if task.MRIID != 1 || task.Head != f.head || task.Title != "Existing task" || task.Control != "Human" {
		t.Fatal(task)
	}
	if _, err = f.a.DB.Exec(f.ctx, `INSERT INTO coding_tasks(id,tenant_id,project_id,owner_id,title,description,criteria,branch,target_branch,mr_iid,head,reviewer_ids) SELECT 'new-'||n,tenant_id,project_id,owner_id,title,description,criteria,'agent/new-'||n,target_branch,0,head,reviewer_ids FROM coding_tasks CROSS JOIN generate_series(1,2) n WHERE id='legacy'`); err != nil {
		t.Fatal(err)
	}
}
