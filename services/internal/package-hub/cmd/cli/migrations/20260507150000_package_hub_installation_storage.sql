-- +goose Up
CREATE TABLE package_hub_package_installations (
    id uuid PRIMARY KEY,
    package_id uuid NOT NULL REFERENCES package_hub_packages(id),
    package_version_id uuid NOT NULL REFERENCES package_hub_package_versions(id),
    scope_type text NOT NULL,
    scope_ref text NOT NULL,
    installation_status text NOT NULL,
    desired_state text NOT NULL,
    runtime_requirement_digest text NOT NULL DEFAULT '',
    secret_binding_status text NOT NULL,
    last_health_status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT package_hub_package_installations_scope_type_chk
        CHECK (scope_type IN ('platform', 'organization', 'project', 'repository')),
    CONSTRAINT package_hub_package_installations_scope_ref_chk CHECK (scope_ref <> ''),
    CONSTRAINT package_hub_package_installations_status_chk
        CHECK (installation_status IN ('requested', 'active', 'disabled', 'failed', 'uninstalled')),
    CONSTRAINT package_hub_package_installations_desired_state_chk
        CHECK (desired_state IN ('present', 'absent', 'suspended')),
    CONSTRAINT package_hub_package_installations_secret_status_chk
        CHECK (secret_binding_status IN ('not_required', 'missing', 'complete', 'invalid')),
    CONSTRAINT package_hub_package_installations_health_status_chk
        CHECK (last_health_status IN ('unknown', 'healthy', 'degraded', 'failed')),
    CONSTRAINT package_hub_package_installations_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX package_hub_package_installations_active_scope_uidx
    ON package_hub_package_installations (package_id, scope_type, scope_ref)
    WHERE installation_status <> 'uninstalled';

CREATE INDEX package_hub_package_installations_scope_status_idx
    ON package_hub_package_installations (scope_type, scope_ref, installation_status);

CREATE INDEX package_hub_package_installations_package_version_scope_idx
    ON package_hub_package_installations (package_id, package_version_id, scope_type, scope_ref);

CREATE INDEX package_hub_package_installations_problem_status_idx
    ON package_hub_package_installations (installation_status, secret_binding_status, last_health_status);

CREATE TABLE package_hub_package_secret_schemas (
    id uuid PRIMARY KEY,
    package_version_id uuid NOT NULL REFERENCES package_hub_package_versions(id),
    schema_digest text NOT NULL,
    fields jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL,
    UNIQUE (package_version_id, schema_digest),
    CONSTRAINT package_hub_package_secret_schemas_digest_chk CHECK (schema_digest <> ''),
    CONSTRAINT package_hub_package_secret_schemas_fields_chk CHECK (jsonb_typeof(fields) = 'array')
);

CREATE INDEX package_hub_package_secret_schemas_version_created_idx
    ON package_hub_package_secret_schemas (package_version_id, created_at DESC);

CREATE TABLE package_hub_package_verifications (
    id uuid PRIMARY KEY,
    package_version_id uuid NOT NULL REFERENCES package_hub_package_versions(id),
    verification_status text NOT NULL,
    verified_by_actor_ref text NOT NULL DEFAULT '',
    verification_notes text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    CONSTRAINT package_hub_package_verifications_status_chk
        CHECK (verification_status IN ('verified', 'unverified', 'rejected', 'revoked'))
);

CREATE INDEX package_hub_package_verifications_version_created_idx
    ON package_hub_package_verifications (package_version_id, created_at DESC);

CREATE INDEX package_hub_package_verifications_status_idx
    ON package_hub_package_verifications (verification_status, created_at DESC);

CREATE TABLE package_hub_command_results (
    key text PRIMARY KEY,
    command_id uuid,
    idempotency_key text NOT NULL DEFAULT '',
    operation text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    result_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL,
    CONSTRAINT package_hub_command_results_key_chk CHECK (key <> ''),
    CONSTRAINT package_hub_command_results_operation_chk CHECK (operation <> ''),
    CONSTRAINT package_hub_command_results_aggregate_type_chk
        CHECK (aggregate_type IN ('package_source', 'package', 'package_version', 'installation', 'verification')),
    CONSTRAINT package_hub_command_results_payload_chk CHECK (jsonb_typeof(result_payload) = 'object'),
    CONSTRAINT package_hub_command_results_identity_chk CHECK (command_id IS NOT NULL OR idempotency_key <> '')
);

CREATE UNIQUE INDEX package_hub_command_results_command_id_uidx
    ON package_hub_command_results (command_id)
    WHERE command_id IS NOT NULL;

CREATE UNIQUE INDEX package_hub_command_results_operation_idempotency_uidx
    ON package_hub_command_results (operation, idempotency_key)
    WHERE command_id IS NULL AND idempotency_key <> '';

CREATE INDEX package_hub_command_results_aggregate_idx
    ON package_hub_command_results (aggregate_type, aggregate_id);

-- +goose Down
DROP TABLE IF EXISTS package_hub_command_results;
DROP TABLE IF EXISTS package_hub_package_verifications;
DROP TABLE IF EXISTS package_hub_package_secret_schemas;
DROP TABLE IF EXISTS package_hub_package_installations;
