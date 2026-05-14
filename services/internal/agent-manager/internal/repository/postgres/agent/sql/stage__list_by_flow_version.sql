-- name: stage__list_by_flow_version :many
SELECT
    id,
    flow_version_id,
    slug,
    stage_type,
    display_name,
    icon_object_uri,
    required_artifacts,
    acceptance_policy,
    position
FROM agent_manager_stages
WHERE flow_version_id = @flow_version_id
ORDER BY position, id;
