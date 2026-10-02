CREATE TABLE IF NOT EXISTS coding_tasks (
 id text PRIMARY KEY, tenant_id text NOT NULL, project_id text NOT NULL,
 owner_id text NOT NULL REFERENCES users(id), title text NOT NULL, description text NOT NULL, criteria text NOT NULL,
 branch text NOT NULL, target_branch text NOT NULL, mr_iid bigint NOT NULL CHECK(mr_iid>0),
 head text NOT NULL, status text NOT NULL DEFAULT 'Review', control text NOT NULL DEFAULT 'Human',
 version bigint NOT NULL DEFAULT 1, epoch bigint NOT NULL DEFAULT 1, round integer NOT NULL DEFAULT 0,
 checks text NOT NULL DEFAULT 'NotRun', checked_head text NOT NULL DEFAULT '', approval_head text NOT NULL DEFAULT '',
 reviewer_ids text[] NOT NULL, active_run text, stop_requested_at timestamptz,
 stop_confirmed boolean NOT NULL DEFAULT true, discussions jsonb NOT NULL DEFAULT '[]',
 synthetic boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(tenant_id,project_id) REFERENCES projects(tenant_id,id), UNIQUE(project_id,mr_iid)
);
CREATE TABLE IF NOT EXISTS coding_runs (
 id text PRIMARY KEY, task_id text NOT NULL REFERENCES coding_tasks(id), epoch bigint NOT NULL,
 head text NOT NULL, status text NOT NULL DEFAULT 'Queued', requirements text NOT NULL,
 claimed_by text NOT NULL DEFAULT '', lease_until timestamptz, output jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now(), finished_at timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS coding_one_active_run ON coding_runs(task_id) WHERE status IN ('Queued','Running','Stopping');
CREATE TABLE IF NOT EXISTS coding_events (
 id bigserial PRIMARY KEY, task_id text NOT NULL REFERENCES coding_tasks(id), actor_id text NOT NULL,
 type text NOT NULL, detail jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO schema_versions(version) VALUES(3) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS coding_feedback (
 id text PRIMARY KEY, task_id text NOT NULL REFERENCES coding_tasks(id), submitted_by text NOT NULL REFERENCES users(id),
 head text NOT NULL, requirements text NOT NULL, status text NOT NULL DEFAULT 'Pending', created_at timestamptz NOT NULL DEFAULT now()
);
