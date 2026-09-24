-- name: configuration_validateassistantplan_select_target_version :one
SELECT version FROM control_plane.agents
WHERE $2='AGENT' AND organization_id=$1::uuid AND ref=$3
UNION ALL
SELECT version FROM control_plane.integration_connections
WHERE $2='INTEGRATION_CONNECTION' AND organization_id=$1::uuid AND ref=$3
UNION ALL
SELECT version FROM control_plane.workflows
WHERE $2='WORKFLOW' AND organization_id=$1::uuid AND ref=$3
UNION ALL
SELECT version FROM control_plane.schedules
WHERE $2='SCHEDULE' AND organization_id=$1::uuid AND ref=$3 AND lifecycle_state='ACTIVE'
UNION ALL
SELECT version FROM control_plane.runtime_environment_sets
WHERE $2='ENVIRONMENT' AND organization_id=$1::uuid AND ref=$3 AND state<>'DELETED'
UNION ALL
SELECT version FROM control_plane.projects
WHERE $2='PROJECT' AND organization_id=$1::uuid AND ref=$3 AND lifecycle='ACTIVE'
LIMIT 1
