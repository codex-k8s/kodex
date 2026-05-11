-- +goose Up
CREATE TABLE provider_hub_artifact_signals (
    id uuid PRIMARY KEY,
    identity_key text NOT NULL,
    provider_slug text NOT NULL,
    external_account_id uuid NOT NULL,
    source text NOT NULL,
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    artifact_kinds_json jsonb NOT NULL,
    target_json jsonb NOT NULL,
    payload_json jsonb NOT NULL,
    observed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (identity_key),
    CONSTRAINT provider_hub_artifact_signals_identity_chk CHECK (identity_key <> ''),
    CONSTRAINT provider_hub_artifact_signals_provider_chk CHECK (provider_slug <> ''),
    CONSTRAINT provider_hub_artifact_signals_source_chk CHECK (source <> ''),
    CONSTRAINT provider_hub_artifact_signals_scope_type_chk
        CHECK (scope_type IN ('repository', 'organization', 'work_item', 'package_source')),
    CONSTRAINT provider_hub_artifact_signals_scope_ref_chk CHECK (scope_ref <> ''),
    CONSTRAINT provider_hub_artifact_signals_artifacts_chk
        CHECK (jsonb_typeof(artifact_kinds_json) = 'array' AND jsonb_array_length(artifact_kinds_json) > 0),
    CONSTRAINT provider_hub_artifact_signals_target_chk CHECK (jsonb_typeof(target_json) = 'object'),
    CONSTRAINT provider_hub_artifact_signals_payload_chk CHECK (jsonb_typeof(payload_json) = 'object')
);

CREATE INDEX provider_hub_artifact_signals_target_idx
    ON provider_hub_artifact_signals (provider_slug, scope_type, scope_ref, observed_at);

CREATE INDEX provider_hub_artifact_signals_account_idx
    ON provider_hub_artifact_signals (external_account_id, observed_at);

-- +goose Down
DROP TABLE IF EXISTS provider_hub_artifact_signals;
