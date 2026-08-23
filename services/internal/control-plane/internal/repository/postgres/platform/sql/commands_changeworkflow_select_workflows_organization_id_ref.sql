-- name: commands_changeworkflow_select_workflows_organization_id_ref :one
SELECT w.id::text,w.project_id::text,p.ref,w.state,w.version FROM control_plane.workflows w JOIN control_plane.projects p ON p.id=w.project_id WHERE w.organization_id=$1::uuid AND w.ref=$2 FOR UPDATE
