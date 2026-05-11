-- name: artifact_signal__get_by_identity :one
SELECT
    id,
    identity_key,
    provider_slug,
    external_account_id,
    source,
    scope_type,
    scope_ref,
    artifact_kinds_json,
    target_json,
    payload_json,
    observed_at,
    created_at
FROM provider_hub_artifact_signals
WHERE identity_key = @identity_key;
