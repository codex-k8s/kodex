-- name: platform__artifacts_changeartifactbinding_select_agents_organization_id_project_id_ref :one
SELECT id::text
FROM control_plane.agents
WHERE organization_id=$1::uuid
  AND project_id=$2::uuid
  AND ref=$3
  AND system_key IS NULL
  AND state<>'ARCHIVED'
FOR UPDATE
