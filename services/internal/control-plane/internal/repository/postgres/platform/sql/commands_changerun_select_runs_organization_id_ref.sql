-- name: platform__commands_changerun_select_runs_organization_id_ref :one
SELECT r.id::text,r.root_run_id::text,r.project_id::text,p.ref,r.state,r.version,r.attempt FROM control_plane.runs r JOIN control_plane.projects p ON p.id=r.project_id WHERE r.organization_id=$1::uuid AND r.ref=$2 FOR UPDATE
