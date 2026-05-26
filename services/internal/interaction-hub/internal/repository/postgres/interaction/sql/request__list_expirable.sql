-- name: request__list_expirable :many
SELECT
    id,
    request_kind,
    scope_type,
    scope_ref,
    thread_id,
    source_owner_kind,
    source_owner_ref,
    ingress_kind,
    ingress_ref,
    decision_owner_kind,
    decision_owner_request_ref,
    decision_owner_decision_ref,
    target_refs,
    context_refs,
    prompt_summary,
    prompt_object_uri,
    prompt_object_digest,
    prompt_object_size_bytes,
    allowed_actions,
    risk_class,
    status,
    deadline_at,
    reminder_policy_ref,
    version,
    created_at,
    updated_at,
    resolved_at
FROM interaction_hub_requests
WHERE scope_type = @scope_type
  AND scope_ref = @scope_ref
  AND deadline_at IS NOT NULL
  AND deadline_at <= @deadline_before
  AND status IN ('created', 'routed', 'waiting')
ORDER BY deadline_at, id
LIMIT @limit::integer;
