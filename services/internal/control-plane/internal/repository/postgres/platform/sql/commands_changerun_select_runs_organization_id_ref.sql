-- name: commands_changerun_select_runs_organization_id_ref :one
SELECT r.id::text,r.root_run_id::text,COALESCE(r.project_id::text,''),COALESCE(p.ref,''),r.state,r.version,r.attempt,r.target_type
FROM control_plane.runs r
LEFT JOIN control_plane.projects p ON p.id=r.project_id
WHERE r.organization_id=$1::uuid AND r.ref=$2
FOR UPDATE OF r
