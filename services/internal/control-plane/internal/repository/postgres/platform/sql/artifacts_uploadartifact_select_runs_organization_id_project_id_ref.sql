-- name: platform__artifacts_uploadartifact_select_runs_organization_id_project_id_ref :one
SELECT r.id::text,r.root_run_id::text,r.ref,s.ref
FROM control_plane.runs r
JOIN control_plane.sessions s ON s.id=r.session_id
WHERE r.organization_id=$1::uuid AND r.project_id=$2::uuid AND r.ref=$3
