-- name: stage_role_binding__list_by_flow_version :many
SELECT
    binding.id,
    binding.stage_id,
    binding.role_profile_id,
    binding.binding_kind,
    binding.launch_policy,
    binding.required_for_acceptance
FROM agent_manager_stage_role_bindings AS binding
JOIN agent_manager_stages AS stage ON stage.id = binding.stage_id
WHERE stage.flow_version_id = @flow_version_id
ORDER BY stage.position, binding.binding_kind, binding.id;
