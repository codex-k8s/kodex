-- name: placement_decision__create :exec
INSERT INTO fleet_manager_placement_decisions (
    id, command_id, request_fingerprint, status, fleet_scope_id, cluster_id,
    project_id, repository_id, runtime_mode, runtime_profile, input_json,
    reason_code, reason_message, used_default_path, created_at
) VALUES (
    @id, @command_id, @request_fingerprint, @status, @fleet_scope_id, @cluster_id,
    @project_id, @repository_id, @runtime_mode, @runtime_profile, @input_json,
    @reason_code, @reason_message, @used_default_path, @created_at
);
