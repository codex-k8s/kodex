-- name: access_decision_audit__create :exec
INSERT INTO access_decision_audit (
    id, subject_type, subject_id, action_key, resource_type, resource_id,
    scope_type, scope_id, request_context,
    decision, reason_code, policy_version, explanation, created_at
) VALUES (
    @id, @subject_type, @subject_id, @action_key, @resource_type, @resource_id,
    @scope_type, @scope_id, @request_context::jsonb,
    @decision, @reason_code, @policy_version, @explanation::jsonb, @created_at
);
