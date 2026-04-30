-- name: access_decision_audit__get_by_id :one
SELECT
    id, subject_type, subject_id, action_key, resource_type, resource_id,
    decision, reason_code, policy_version, explanation, created_at
FROM access_decision_audit
WHERE id = @id;
