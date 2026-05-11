-- name: artifact_signal__insert :one
INSERT INTO provider_hub_artifact_signals (
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
) VALUES (
    @id,
    @identity_key,
    @provider_slug,
    @external_account_id,
    @source,
    @scope_type,
    @scope_ref,
    @artifact_kinds_json,
    @target_json,
    @payload_json,
    @observed_at,
    @created_at
)
ON CONFLICT (identity_key) DO NOTHING
RETURNING
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
    created_at;
