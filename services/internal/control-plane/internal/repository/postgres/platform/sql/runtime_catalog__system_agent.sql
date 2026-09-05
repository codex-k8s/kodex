-- name: runtime_catalog__system_agent :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.agents
    WHERE organization_id = $1::uuid AND ref = $2
      AND project_id IS NULL AND system_key = 'system-assistant'
      AND state <> 'ARCHIVED'
);
