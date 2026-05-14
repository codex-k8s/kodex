-- name: stage_transition__create :exec
INSERT INTO agent_manager_stage_transitions (
    id, flow_version_id, from_stage_id, to_stage_id, condition_payload, follow_up_type, position
) VALUES (
    @id, @flow_version_id, @from_stage_id::uuid, @to_stage_id, @condition_payload, @follow_up_type, @position
);
