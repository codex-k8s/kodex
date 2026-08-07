-- name: team_work_scope__next :one
-- params:
SELECT organization_id::text, project_id::text
FROM interaction_gateway_next_work_scope('TEAM_OPERATION');
