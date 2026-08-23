-- name: configuration_changeschedule_select_workflow_target :one
SELECT id::text
FROM control_plane.workflows
WHERE organization_id = $1::uuid
  AND project_id = $2::uuid
  AND ref = $3
  AND state = 'PUBLISHED'
