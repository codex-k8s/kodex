-- name: platform__commands_changeagentbinding_1 :one
SELECT a.project_id::text,p.ref,a.version FROM control_plane.agents a JOIN control_plane.projects p ON p.id=a.project_id WHERE a.organization_id=$1::uuid AND a.ref=$2 AND a.system_key IS NULL FOR UPDATE
