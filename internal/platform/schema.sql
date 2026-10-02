CREATE TABLE IF NOT EXISTS schema_versions(version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS users (
 id text PRIMARY KEY, issuer text NOT NULL, subject text, username text NOT NULL UNIQUE,
 name text NOT NULL, email text NOT NULL UNIQUE, status text NOT NULL CHECK(status IN ('invited','active','disabled')),
 operator boolean NOT NULL DEFAULT false, gitlab_id bigint UNIQUE, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(issuer,subject)
);
CREATE TABLE IF NOT EXISTS tenants (
 id text PRIMARY KEY, name text NOT NULL, slug text NOT NULL UNIQUE,
 status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','suspended')),
 version bigint NOT NULL DEFAULT 1, gitlab_id bigint UNIQUE, last_checked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS teams (
 id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id), name text NOT NULL,
 UNIQUE(tenant_id,name), UNIQUE(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS memberships (
 id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id), user_id text NOT NULL REFERENCES users(id),
 role text NOT NULL CHECK(role IN ('admin','member')), status text NOT NULL CHECK(status IN ('invited','active','removed')),
 team_id text, UNIQUE(tenant_id,user_id), UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,team_id) REFERENCES teams(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS projects (
 id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id), name text NOT NULL, path text NOT NULL,
 mode text NOT NULL CHECK(mode IN ('create','bind')), gitlab_id bigint UNIQUE,
 UNIQUE(tenant_id,path), UNIQUE(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS grants (
 tenant_id text NOT NULL, project_id text NOT NULL, membership_id text NOT NULL,
 desired_level integer NOT NULL CHECK(desired_level IN (0,20,30,40)), observed_level integer NOT NULL DEFAULT 0,
 checked_at timestamptz, PRIMARY KEY(project_id,membership_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES projects(tenant_id,id),
 FOREIGN KEY(tenant_id,membership_id) REFERENCES memberships(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS operations (
 id text PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id), version bigint NOT NULL,
 status text NOT NULL CHECK(status IN ('pending','running','retrying','failed','conflict','succeeded','superseded')),
 attempts integer NOT NULL DEFAULT 0, next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz, error_code text NOT NULL DEFAULT '', error_message text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(tenant_id,version)
);
CREATE INDEX IF NOT EXISTS operations_ready ON operations(next_attempt_at) WHERE status IN ('pending','retrying','running');
CREATE TABLE IF NOT EXISTS audit_events (
 id bigserial PRIMARY KEY, tenant_id text REFERENCES tenants(id), actor_id text NOT NULL,
 action text NOT NULL, target text NOT NULL, detail jsonb NOT NULL DEFAULT '{}', operation_id text,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS invitations (
 id text PRIMARY KEY, token_hash text NOT NULL UNIQUE, membership_id text NOT NULL REFERENCES memberships(id),
 expires_at timestamptz NOT NULL, consumed_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS sessions (
 token_hash text PRIMARY KEY, user_id text NOT NULL REFERENCES users(id), csrf_hash text NOT NULL,
 expires_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS login_flows (
 token_hash text PRIMARY KEY, state_hash text NOT NULL, nonce text NOT NULL, verifier text NOT NULL,
 return_path text NOT NULL, expires_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS idempotency (
 actor_id text NOT NULL, request_key text NOT NULL, route text NOT NULL, body_hash text NOT NULL,
 status_code integer NOT NULL, response bytea NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(actor_id,request_key)
);
CREATE TABLE IF NOT EXISTS access_events (
 id bigserial PRIMARY KEY, tenant_id text NOT NULL REFERENCES tenants(id), version bigint NOT NULL,
 reason text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO schema_versions(version) VALUES(1) ON CONFLICT DO NOTHING;

ALTER TABLE grants ADD COLUMN IF NOT EXISTS effective_level integer NOT NULL DEFAULT 0 CHECK(effective_level IN (0,20,30,40));
INSERT INTO schema_versions(version) VALUES(2) ON CONFLICT DO NOTHING;
