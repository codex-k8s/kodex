-- name: stage_role_binding__create :exec
INSERT INTO agent_manager_stage_role_bindings (
    id, stage_id, role_profile_id, binding_kind, launch_policy, required_for_acceptance
) VALUES (
    @id, @stage_id, @role_profile_id, @binding_kind, @launch_policy, @required_for_acceptance
);
