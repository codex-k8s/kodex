-- name: provider_financial_agents :many
SELECT ref
FROM control_plane.agents
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND state <> 'ARCHIVED'
ORDER BY ref;
