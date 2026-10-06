-- name: workflow_launch__lock_projects :many
WITH targets AS MATERIALIZED (
 SELECT project.id
 FROM control_plane.projects project
 WHERE project.organization_id=@organization_id::uuid AND (
   project.ref=@project_ref OR project.id IN (
     SELECT run.project_id FROM control_plane.runs run WHERE run.organization_id=@organization_id::uuid AND run.ref=@locator
     UNION SELECT run.project_id FROM control_plane.runtime_leases lease JOIN control_plane.runs run ON run.id=lease.run_id WHERE lease.organization_id=@organization_id::uuid AND lease.ref=@locator
     UNION SELECT gate.project_id FROM control_plane.owner_gates gate WHERE gate.organization_id=@organization_id::uuid AND gate.ref=@locator
     UNION SELECT session.project_id FROM control_plane.sessions session WHERE session.organization_id=@organization_id::uuid AND session.ref=@locator
   ) OR (@claim AND EXISTS (SELECT 1 FROM control_plane.required_workflow_launches launch WHERE launch.organization_id=project.organization_id AND launch.project_id=project.id AND launch.state='OPEN'))
 ) ORDER BY project.id
)
SELECT id::text, pg_advisory_xact_lock(hashtextextended('required-workflow:'||@organization_id||':'||id::text, 0)) FROM targets ORDER BY id;
