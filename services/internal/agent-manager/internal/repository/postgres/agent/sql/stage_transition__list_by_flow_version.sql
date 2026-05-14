-- name: stage_transition__list_by_flow_version :many
SELECT
    id,
    flow_version_id,
    from_stage_id,
    to_stage_id,
    condition_payload,
    follow_up_type,
    position
FROM agent_manager_stage_transitions
WHERE flow_version_id = @flow_version_id
ORDER BY position, id;
