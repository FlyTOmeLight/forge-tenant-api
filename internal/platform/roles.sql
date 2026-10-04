CREATE TABLE IF NOT EXISTS team_memberships (
 tenant_id text NOT NULL, team_id text NOT NULL, membership_id text NOT NULL,
 PRIMARY KEY(team_id,membership_id),
 FOREIGN KEY(tenant_id,team_id) REFERENCES teams(tenant_id,id),
 FOREIGN KEY(tenant_id,membership_id) REFERENCES memberships(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS direct_grants (
 tenant_id text NOT NULL, project_id text NOT NULL, membership_id text NOT NULL,
 level integer NOT NULL CHECK(level IN (20,30,40)), PRIMARY KEY(project_id,membership_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES projects(tenant_id,id),
 FOREIGN KEY(tenant_id,membership_id) REFERENCES memberships(tenant_id,id)
);
CREATE TABLE IF NOT EXISTS team_grants (
 tenant_id text NOT NULL, project_id text NOT NULL, team_id text NOT NULL,
 level integer NOT NULL CHECK(level IN (20,30,40)), PRIMARY KEY(project_id,team_id),
 FOREIGN KEY(tenant_id,project_id) REFERENCES projects(tenant_id,id),
 FOREIGN KEY(tenant_id,team_id) REFERENCES teams(tenant_id,id)
);
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM schema_versions WHERE version=5) THEN
  INSERT INTO team_memberships SELECT tenant_id,team_id,id FROM memberships WHERE team_id IS NOT NULL AND status<>'removed';
  INSERT INTO direct_grants SELECT tenant_id,project_id,membership_id,desired_level FROM grants WHERE desired_level>0;
  ALTER TABLE memberships DROP COLUMN team_id;
  INSERT INTO schema_versions(version) VALUES(5);
 END IF;
END $$;
CREATE INDEX IF NOT EXISTS team_memberships_member ON team_memberships(tenant_id,membership_id);
