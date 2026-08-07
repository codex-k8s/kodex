-- +goose Up
-- Целевая owner schema Issue #236. До one-shot cutover #196 эти таблицы не
-- синхронизируются с legacy bot-service и не обслуживают runtime сессии.

ALTER TABLE integration_gateway.approvals
    ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);

CREATE TABLE integration_gateway.managed_provider_connections (
    connection_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    stable_key text NOT NULL,
    provider_id text NOT NULL,
    display_name text NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    generation bigint NOT NULL CHECK (generation > 0),
    revoke_generation bigint NOT NULL DEFAULT 0 CHECK (revoke_generation >= 0),
    status text NOT NULL CHECK (status IN ('PENDING', 'VALID', 'INVALID', 'REVOKED')),
    active_credential_generation bigint NOT NULL DEFAULT 0 CHECK (active_credential_generation >= 0),
    masked_label text NOT NULL DEFAULT '',
    masked_account text NOT NULL DEFAULT '',
    capability_sha256 text NOT NULL CHECK (capability_sha256 ~ '^[0-9a-f]{64}$'),
    observation_sha256 text NOT NULL CHECK (observation_sha256 ~ '^[0-9a-f]{64}$'),
    observed_at timestamptz,
    control_plane_resource_id text NOT NULL DEFAULT '',
    control_plane_version bigint NOT NULL DEFAULT 0 CHECK (control_plane_version >= 0),
    control_plane_sha256 text NOT NULL DEFAULT '',
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id, project_id, stable_key)
);
CREATE INDEX managed_provider_connections_scope_idx
    ON integration_gateway.managed_provider_connections(tenant_id, project_id, updated_at DESC);

CREATE TABLE integration_gateway.provider_authorization_attempts (
    authorization_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    connection_id text NOT NULL REFERENCES integration_gateway.managed_provider_connections(connection_id),
    provider_id text NOT NULL,
    attempt bigint NOT NULL CHECK (attempt > 0),
    version bigint NOT NULL CHECK (version > 0),
    generation bigint NOT NULL CHECK (generation > 0),
    state text NOT NULL CHECK (state IN ('PENDING', 'CODE_ISSUED', 'AUTHORIZED', 'DENIED', 'EXPIRED', 'FAILED', 'CANCELLED')),
    intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
    provider_login_id_ciphertext bytea NOT NULL DEFAULT ''::bytea,
    device_result_ciphertext bytea NOT NULL DEFAULT ''::bytea,
    code_expires_at timestamptz,
    expires_at timestamptz NOT NULL,
    failure_category text NOT NULL DEFAULT '',
    lease_id text NOT NULL DEFAULT '',
    lease_generation bigint NOT NULL DEFAULT 0 CHECK (lease_generation >= 0),
    lease_expires_at timestamptz,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (connection_id, attempt)
);
CREATE UNIQUE INDEX provider_authorization_open_uidx
    ON integration_gateway.provider_authorization_attempts(connection_id)
    WHERE state IN ('PENDING', 'CODE_ISSUED');

CREATE TABLE integration_gateway.provider_credential_generations (
    connection_id text NOT NULL REFERENCES integration_gateway.managed_provider_connections(connection_id),
    generation bigint NOT NULL CHECK (generation > 0),
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    authorization_id text NOT NULL UNIQUE REFERENCES integration_gateway.provider_authorization_attempts(authorization_id),
    status text NOT NULL CHECK (status IN ('PENDING', 'ACTIVE', 'REVOKED', 'FAILED')),
    secret_ref text NOT NULL,
    secret_version bigint NOT NULL CHECK (secret_version > 0),
    secret_content_sha256 text NOT NULL CHECK (secret_content_sha256 ~ '^[0-9a-f]{64}$'),
    credential_binding_id text NOT NULL,
    credential_binding_version bigint NOT NULL CHECK (credential_binding_version > 0),
    credential_binding_sha256 text NOT NULL CHECK (credential_binding_sha256 ~ '^[0-9a-f]{64}$'),
    masked_account text NOT NULL DEFAULT '',
    masked_label text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    activated_at timestamptz,
    revoked_at timestamptz,
    PRIMARY KEY (connection_id, generation)
);

CREATE TABLE integration_gateway.managed_provider_pools (
    provider_pool_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    stable_key text NOT NULL,
    display_name text NOT NULL,
    policy text NOT NULL CHECK (policy IN ('LEAST_USED', 'WEIGHTED')),
    version bigint NOT NULL CHECK (version > 0),
    desired_sha256 text NOT NULL CHECK (desired_sha256 ~ '^[0-9a-f]{64}$'),
    observation_version bigint NOT NULL CHECK (observation_version > 0),
    observation_sha256 text NOT NULL CHECK (observation_sha256 ~ '^[0-9a-f]{64}$'),
    effective_sha256 text NOT NULL CHECK (effective_sha256 ~ '^[0-9a-f]{64}$'),
    status text NOT NULL CHECK (status IN ('ACTIVE', 'ARCHIVED', 'DELETED', 'PENDING')),
    control_plane_resource_id text NOT NULL DEFAULT '',
    control_plane_version bigint NOT NULL DEFAULT 0 CHECK (control_plane_version >= 0),
    control_plane_sha256 text NOT NULL DEFAULT '',
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id, project_id, stable_key)
);

CREATE TABLE integration_gateway.integration_configurations (
    configuration_id text NOT NULL,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    stable_key text NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    configuration_sha256 text NOT NULL CHECK (configuration_sha256 ~ '^[0-9a-f]{64}$'),
    definition_id text NOT NULL,
    definition_version bigint NOT NULL CHECK (definition_version > 0),
    definition_sha256 text NOT NULL CHECK (definition_sha256 ~ '^[0-9a-f]{64}$'),
    connection_id text NOT NULL REFERENCES integration_gateway.managed_provider_connections(connection_id),
    connection_version bigint NOT NULL CHECK (connection_version > 0),
    connection_generation bigint NOT NULL CHECK (connection_generation > 0),
    capability_sha256 text NOT NULL CHECK (capability_sha256 ~ '^[0-9a-f]{64}$'),
    effect_kind text NOT NULL CHECK (effect_kind IN ('MCP_TOOL', 'CLI', 'ENVIRONMENT')),
    status text NOT NULL CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (configuration_id, version),
    UNIQUE (tenant_id, project_id, stable_key, version)
);

CREATE TABLE integration_gateway.integration_test_receipts (
    test_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    connection_id text NOT NULL REFERENCES integration_gateway.managed_provider_connections(connection_id),
    connection_version bigint NOT NULL CHECK (connection_version > 0),
    connection_generation bigint NOT NULL CHECK (connection_generation > 0),
    definition_id text NOT NULL,
    definition_version bigint NOT NULL CHECK (definition_version > 0),
    category text NOT NULL CHECK (category IN ('PENDING', 'OK', 'CREDENTIAL_UNAVAILABLE', 'UNAUTHORIZED', 'FORBIDDEN', 'ENDPOINT_UNAVAILABLE', 'TIMEOUT', 'PROTOCOL_ERROR')),
    receipt_sha256 text NOT NULL CHECK (receipt_sha256 ~ '^[0-9a-f]{64}$'),
    expires_at timestamptz NOT NULL,
    tested_at timestamptz,
    created_at timestamptz NOT NULL
);

CREATE TABLE integration_gateway.git_source_bindings (
    binding_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    stable_key text NOT NULL,
    version bigint NOT NULL CHECK (version > 0),
    status text NOT NULL CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    repository_key text NOT NULL,
    ref_key text NOT NULL,
    path_key text NOT NULL,
    repository_connection_id text NOT NULL,
    repository_connection_version bigint NOT NULL CHECK (repository_connection_version > 0),
    repository_connection_sha256 text NOT NULL CHECK (repository_connection_sha256 ~ '^[0-9a-f]{64}$'),
    credential_binding_id text NOT NULL,
    credential_binding_version bigint NOT NULL CHECK (credential_binding_version > 0),
    credential_binding_sha256 text NOT NULL CHECK (credential_binding_sha256 ~ '^[0-9a-f]{64}$'),
    target_kind text NOT NULL CHECK (target_kind IN ('ROLE_DEFINITION', 'AGENT', 'INSTRUCTION_SET', 'PROVIDER_POOL')),
    target_stable_key text NOT NULL,
    fetched_commit text NOT NULL DEFAULT '',
    source_revision bigint NOT NULL DEFAULT 0 CHECK (source_revision >= 0),
    source_sha256 text NOT NULL DEFAULT '',
    fetched_at timestamptz,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id, project_id, stable_key)
);

CREATE TABLE integration_gateway.git_reconciliations (
    reconciliation_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    binding_id text NOT NULL REFERENCES integration_gateway.git_source_bindings(binding_id),
    binding_version bigint NOT NULL CHECK (binding_version > 0),
    state text NOT NULL CHECK (state IN ('PENDING', 'FETCHED', 'APPLIED', 'FAILED', 'CANCELLED')),
    fetched_commit text NOT NULL DEFAULT '',
    source_revision bigint NOT NULL DEFAULT 0 CHECK (source_revision >= 0),
    source_sha256 text NOT NULL DEFAULT '',
    encrypted_snapshot bytea NOT NULL DEFAULT ''::bytea,
    target_resource_id text NOT NULL DEFAULT '',
    target_version bigint NOT NULL DEFAULT 0 CHECK (target_version >= 0),
    target_sha256 text NOT NULL DEFAULT '',
    command_intent_sha256 text NOT NULL DEFAULT '' CHECK (command_intent_sha256 = '' OR command_intent_sha256 ~ '^[0-9a-f]{64}$'),
    receipt_id text NOT NULL,
    receipt_sha256 text NOT NULL CHECK (receipt_sha256 ~ '^[0-9a-f]{64}$'),
    failure_category text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (state = 'PENDING' OR command_intent_sha256 ~ '^[0-9a-f]{64}$'),
    UNIQUE (binding_id, binding_version, source_revision)
);

CREATE TABLE integration_gateway.management_idempotency_receipts (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    operation text NOT NULL,
    key_sha256 text NOT NULL CHECK (key_sha256 ~ '^[0-9a-f]{64}$'),
    request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[0-9a-f]{64}$'),
    resource_kind text NOT NULL,
    resource_id text NOT NULL,
    result_version bigint NOT NULL CHECK (result_version > 0),
    result_sha256 text NOT NULL CHECK (result_sha256 ~ '^[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_id, operation, key_sha256)
);

CREATE TABLE integration_gateway.management_effects (
    effect_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    actor_id text NOT NULL,
    effect_kind text NOT NULL CHECK (effect_kind IN ('PROVIDER_AUTHORIZE', 'PROVIDER_REVOKE', 'PROVIDER_REFERENCE_SYNC', 'PROVIDER_POOL_SYNC', 'INTEGRATION_TEST', 'GIT_FETCH', 'GIT_APPLY')),
    resource_kind text NOT NULL,
    resource_id text NOT NULL,
    resource_version bigint NOT NULL CHECK (resource_version > 0),
    resource_generation bigint NOT NULL DEFAULT 0 CHECK (resource_generation >= 0),
    intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
    status text NOT NULL CHECK (status IN ('PENDING', 'CLAIMED', 'SUCCEEDED', 'FAILED', 'UNKNOWN', 'CANCELLED')),
    available_at timestamptz NOT NULL,
    lease_id text NOT NULL DEFAULT '',
    lease_fence bigint NOT NULL DEFAULT 0 CHECK (lease_fence >= 0),
    lease_expires_at timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (resource_kind, resource_id, resource_version, effect_kind)
);
CREATE INDEX management_effects_claim_idx
    ON integration_gateway.management_effects(status, available_at, effect_kind, effect_id);

-- Selector не раскрывает business payload и возвращает только scope ожидающей
-- работы. Worker после этого активирует signed transaction context и заново
-- проверяет resource version/generation под row lock.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.next_management_scope(
    OUT tenant_id text, OUT project_id text, OUT actor_id text
) RETURNS record
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'integration_gateway_runtime', 'member') THEN
        RAISE EXCEPTION 'runtime principal is not active' USING ERRCODE = '28000';
    END IF;
    SELECT effect.tenant_id, effect.project_id, effect.actor_id
      INTO tenant_id, project_id, actor_id
      FROM integration_gateway.management_effects AS effect
     WHERE (effect.status = 'PENDING' AND effect.available_at <= clock_timestamp())
        OR (effect.status = 'CLAIMED' AND effect.lease_expires_at <= clock_timestamp())
     ORDER BY effect.available_at, effect.effect_id
     LIMIT 1;
END
$function$;
-- +goose StatementEnd

ALTER TABLE integration_gateway.managed_provider_connections ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.managed_provider_connections FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.provider_authorization_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.provider_authorization_attempts FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.provider_credential_generations ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.provider_credential_generations FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.managed_provider_pools ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.managed_provider_pools FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.integration_configurations ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.integration_configurations FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.integration_test_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.integration_test_receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.git_source_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.git_source_bindings FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.git_reconciliations ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.git_reconciliations FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.management_idempotency_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.management_idempotency_receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.management_effects ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.management_effects FORCE ROW LEVEL SECURITY;

CREATE POLICY managed_provider_connections_runtime_scope ON integration_gateway.managed_provider_connections
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY provider_authorization_attempts_runtime_scope ON integration_gateway.provider_authorization_attempts
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY provider_credential_generations_runtime_scope ON integration_gateway.provider_credential_generations
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY managed_provider_pools_runtime_scope ON integration_gateway.managed_provider_pools
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY integration_configurations_runtime_scope ON integration_gateway.integration_configurations
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY integration_test_receipts_runtime_scope ON integration_gateway.integration_test_receipts
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY git_source_bindings_runtime_scope ON integration_gateway.git_source_bindings
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY git_reconciliations_runtime_scope ON integration_gateway.git_reconciliations
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY management_idempotency_receipts_runtime_scope ON integration_gateway.management_idempotency_receipts
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));
CREATE POLICY management_effects_runtime_scope ON integration_gateway.management_effects
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));

GRANT SELECT, INSERT, UPDATE ON integration_gateway.managed_provider_connections TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.provider_authorization_attempts TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.provider_credential_generations TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.managed_provider_pools TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.integration_configurations TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.integration_test_receipts TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.git_source_bindings TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.git_reconciliations TO integration_gateway_runtime;
GRANT SELECT, INSERT ON integration_gateway.management_idempotency_receipts TO integration_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.management_effects TO integration_gateway_runtime;
GRANT EXECUTE ON FUNCTION integration_gateway.next_management_scope() TO integration_gateway_runtime;

-- Readiness использует тот же защищённый runtime path и проверяет точную
-- материализацию schema, не полагаясь на недоступную runtime-роли таблицу goose.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.check_management_readiness()
RETURNS boolean
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
    SELECT pg_catalog.to_regclass('integration_gateway.management_effects') IS NOT NULL
       AND pg_catalog.to_regclass('integration_gateway.provider_credential_generations') IS NOT NULL
       AND pg_catalog.to_regclass('integration_gateway.git_reconciliations') IS NOT NULL
       AND pg_catalog.has_table_privilege(session_user, 'integration_gateway.management_effects', 'SELECT,INSERT,UPDATE')
$function$;
-- +goose StatementEnd
ALTER FUNCTION integration_gateway.check_management_readiness() OWNER TO integration_gateway_owner;
REVOKE ALL ON FUNCTION integration_gateway.check_management_readiness() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION integration_gateway.check_management_readiness() TO integration_gateway_runtime;

ALTER TABLE integration_gateway.managed_provider_connections OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.provider_authorization_attempts OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.provider_credential_generations OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.managed_provider_pools OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.integration_configurations OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.integration_test_receipts OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.git_source_bindings OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.git_reconciliations OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.management_idempotency_receipts OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.management_effects OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.next_management_scope() OWNER TO integration_gateway_owner;

-- +goose Down
-- Forward-only owner state и immutable generations не удаляются.
SELECT 1;
