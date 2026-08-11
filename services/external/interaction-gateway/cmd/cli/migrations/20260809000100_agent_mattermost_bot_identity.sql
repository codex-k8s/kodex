-- +goose Up
RESET ROLE;
SET ROLE interaction_gateway_owner;
ALTER TABLE interaction_gateway_deliveries
    ADD COLUMN bot_provider_user_id text NOT NULL DEFAULT '' CHECK (length(bot_provider_user_id) <= 64),
    ADD COLUMN bot_provider_generation bigint NOT NULL DEFAULT 0 CHECK (bot_provider_generation >= 0);
ALTER TABLE interaction_gateway_download_grants
    ADD COLUMN bot_stable_key text NOT NULL DEFAULT '' CHECK (length(bot_stable_key) <= 128),
    ADD COLUMN bot_provider_user_id text NOT NULL DEFAULT '' CHECK (length(bot_provider_user_id) <= 64),
    ADD COLUMN bot_provider_generation bigint NOT NULL DEFAULT 0 CHECK (bot_provider_generation >= 0);

CREATE TABLE interaction_gateway_agent_bot_metadata (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    schema_version bigint NOT NULL CHECK (schema_version = 1)
);
INSERT INTO interaction_gateway_agent_bot_metadata(singleton, schema_version) VALUES (true, 1);

CREATE TABLE interaction_gateway_agent_bot_identities (
    identity_ref uuid PRIMARY KEY,
    provider_object_ref uuid NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    agent_ref uuid,
    agent_stable_key text NOT NULL DEFAULT '' CHECK (length(agent_stable_key) <= 128),
    provider_bot_id text NOT NULL CHECK (length(provider_bot_id) BETWEEN 1 AND 64),
    provider_user_id text NOT NULL CHECK (length(provider_user_id) BETWEEN 1 AND 64),
    provider_team_id text NOT NULL CHECK (length(provider_team_id) BETWEEN 1 AND 64),
    provider_token_id text NOT NULL DEFAULT '' CHECK (length(provider_token_id) <= 64),
    credential_binding_id uuid,
    credential_secret_ref text NOT NULL DEFAULT '' CHECK (length(credential_secret_ref) <= 512),
    credential_secret_version bigint CHECK (credential_secret_version IS NULL OR credential_secret_version > 0),
    credential_sha256 text NOT NULL DEFAULT '' CHECK (credential_sha256 = '' OR credential_sha256 ~ '^[0-9a-f]{64}$'),
    username text NOT NULL CHECK (username ~ '^[a-z][a-z0-9._-]{2,21}$'),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 64),
    status text NOT NULL CHECK (status IN ('AVAILABLE', 'REVOKED', 'DELETED', 'UNKNOWN')),
    provider_version bigint NOT NULL CHECK (provider_version > 0),
    provider_generation bigint CHECK (provider_generation IS NULL OR provider_generation > 0),
    provider_snapshot_sha256 text NOT NULL CHECK (provider_snapshot_sha256 ~ '^[0-9a-f]{64}$'),
    provider_causality_sha256 text NOT NULL DEFAULT '' CHECK (provider_causality_sha256 = '' OR provider_causality_sha256 ~ '^[0-9a-f]{64}$'),
    observed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, project_id, agent_ref, provider_generation),
    CHECK ((agent_ref IS NULL AND agent_stable_key = '') OR
           (agent_ref IS NOT NULL AND length(agent_stable_key) BETWEEN 1 AND 128))
);

CREATE TABLE interaction_gateway_agent_bot_bindings (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    agent_ref uuid NOT NULL,
    actor_id uuid NOT NULL,
    agent_stable_key text NOT NULL CHECK (length(agent_stable_key) BETWEEN 1 AND 128),
    agent_version bigint NOT NULL CHECK (agent_version > 0),
    identity_ref uuid NOT NULL REFERENCES interaction_gateway_agent_bot_identities(identity_ref),
    provider_generation bigint NOT NULL CHECK (provider_generation > 0),
    status text NOT NULL CHECK (status IN ('AVAILABLE', 'REVOKED')),
    receipt_sha256 text NOT NULL CHECK (receipt_sha256 ~ '^[0-9a-f]{64}$'),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, project_id, agent_ref)
);

CREATE TABLE interaction_gateway_agent_bot_ownership (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    provider_object_ref uuid NOT NULL,
    agent_ref uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, project_id, provider_object_ref),
    UNIQUE (organization_id, project_id, agent_ref, provider_object_ref)
);

CREATE TABLE interaction_gateway_agent_bot_watermarks (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    agent_ref uuid NOT NULL,
    provider_generation bigint NOT NULL CHECK (provider_generation > 0),
    admitted boolean NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, project_id, agent_ref)
);

CREATE TABLE interaction_gateway_agent_bot_selectors (
    selector_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    identity_ref uuid NOT NULL REFERENCES interaction_gateway_agent_bot_identities(identity_ref),
    provider_snapshot_sha256 text NOT NULL CHECK (provider_snapshot_sha256 ~ '^[0-9a-f]{64}$'),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, project_id, actor_id, identity_ref)
);

CREATE TABLE interaction_gateway_agent_bot_catalog_cursors (
    cursor_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    catalog_offset integer NOT NULL CHECK (catalog_offset BETWEEN 0 AND 10000),
    page_size integer NOT NULL CHECK (page_size BETWEEN 1 AND 100),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE interaction_gateway_agent_bot_operations (
    operation_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('create_and_bind', 'bind', 'rebind', 'revoke')),
    idempotency_key uuid NOT NULL,
    agent_ref uuid NOT NULL,
    expected_agent_version bigint NOT NULL CHECK (expected_agent_version > 0),
    predecessor_generation bigint NOT NULL CHECK (predecessor_generation >= 0),
    identity_ref uuid,
    selector_id uuid,
    request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[0-9a-f]{64}$'),
    username text NOT NULL DEFAULT '' CHECK (username = '' OR username ~ '^[a-z][a-z0-9._-]{2,21}$'),
    display_name text NOT NULL DEFAULT '' CHECK (length(display_name) <= 64),
    provider_correlation uuid,
    state text NOT NULL CHECK (state IN ('EFFECT_PENDING', 'MEMBERSHIP_PENDING', 'AMBIGUOUS',
        'PROVIDER_ACCEPTED', 'BOUND', 'REVOKED', 'REPAIR_REQUIRED')),
    receipt_id uuid,
    receipt_revision bigint CHECK (receipt_revision IS NULL OR receipt_revision > 0),
    receipt_sha256 text NOT NULL DEFAULT '' CHECK (receipt_sha256 = '' OR receipt_sha256 ~ '^[0-9a-f]{64}$'),
    command_intent_sha256 text NOT NULL DEFAULT '' CHECK (command_intent_sha256 = '' OR command_intent_sha256 ~ '^[0-9a-f]{64}$'),
    result_agent_version bigint CHECK (result_agent_version IS NULL OR result_agent_version > 0),
    failure_code text NOT NULL DEFAULT '' CHECK (failure_code ~ '^[A-Z0-9_]{0,64}$'),
    fence bigint NOT NULL CHECK (fence > 0),
    lease_owner text NOT NULL CHECK (length(lease_owner) BETWEEN 1 AND 128),
    lease_token_sha256 text NOT NULL CHECK (lease_token_sha256 ~ '^[0-9a-f]{64}$'),
    lease_expires_at timestamptz NOT NULL,
    effect_started_at timestamptz,
    retry_not_before timestamptz NOT NULL DEFAULT clock_timestamp(),
    recovery_deadline timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, project_id, actor_id, agent_ref, action, idempotency_key),
    UNIQUE (organization_id, project_id, agent_ref, expected_agent_version, predecessor_generation),
    CHECK ((action = 'create_and_bind' AND username <> '' AND display_name <> '' AND provider_correlation IS NOT NULL
            AND predecessor_generation = 0) OR
           (action = 'bind' AND predecessor_generation = 0) OR
           (action IN ('rebind', 'revoke') AND predecessor_generation > 0)),
    CHECK (state <> 'EFFECT_PENDING' OR action IN ('create_and_bind', 'revoke')),
    CHECK (state NOT IN ('BOUND', 'REVOKED') OR
        (identity_ref IS NOT NULL AND receipt_id IS NOT NULL AND receipt_revision IS NOT NULL
         AND receipt_sha256 <> '' AND command_intent_sha256 <> '' AND result_agent_version IS NOT NULL))
);
CREATE INDEX interaction_gateway_agent_bot_operation_due_idx
    ON interaction_gateway_agent_bot_operations(retry_not_before, created_at)
    WHERE state IN ('EFFECT_PENDING', 'MEMBERSHIP_PENDING', 'AMBIGUOUS', 'PROVIDER_ACCEPTED');

CREATE TABLE interaction_gateway_agent_bot_work_scopes (
    operation_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    due_at timestamptz NOT NULL
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_sync_agent_bot_work_scope()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
BEGIN
    IF NEW.state IN ('EFFECT_PENDING', 'MEMBERSHIP_PENDING', 'AMBIGUOUS', 'PROVIDER_ACCEPTED') THEN
        INSERT INTO interaction_gateway_agent_bot_work_scopes(operation_id, organization_id, project_id, due_at)
        VALUES (NEW.operation_id, NEW.organization_id, NEW.project_id,
            GREATEST(NEW.retry_not_before, NEW.lease_expires_at))
        ON CONFLICT (operation_id) DO UPDATE SET due_at = EXCLUDED.due_at;
    ELSE
        DELETE FROM interaction_gateway_agent_bot_work_scopes WHERE operation_id = NEW.operation_id;
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd
CREATE TRIGGER interaction_gateway_agent_bot_work_scope_trigger
AFTER INSERT OR UPDATE OF state, retry_not_before, lease_expires_at
ON interaction_gateway_agent_bot_operations
FOR EACH ROW EXECUTE FUNCTION interaction_gateway_sync_agent_bot_work_scope();

-- Расширение существующего SECURITY DEFINER locator сохраняет payload за RLS.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_next_work_scope(requested_kind text,
    OUT organization_id uuid, OUT project_id uuid) RETURNS record
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'interaction_gateway_runtime', 'member') OR NOT EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals AS principal
        JOIN interaction_gateway_runtime_credential_fence AS fence ON fence.singleton
        WHERE principal.principal_name::text = session_user AND principal.status = 'CURRENT'
          AND principal.generation = fence.served_generation
    ) THEN
        RAISE EXCEPTION 'runtime identity is not active' USING ERRCODE = '28000';
    END IF;
    IF requested_kind = 'INBOUND' THEN
        SELECT scope.organization_id, scope.project_id INTO organization_id, project_id
        FROM interaction_gateway_inbound_work_scopes AS scope
        WHERE scope.due_at <= clock_timestamp() ORDER BY scope.due_at, scope.inbound_id LIMIT 1;
    ELSIF requested_kind = 'DELIVERY' THEN
        SELECT scope.organization_id, scope.project_id INTO organization_id, project_id
        FROM interaction_gateway_delivery_work_scopes AS scope
        WHERE scope.delivery_active AND scope.due_at <= clock_timestamp()
        ORDER BY scope.due_at, scope.delivery_id LIMIT 1;
    ELSIF requested_kind = 'TURN_WATCH' THEN
        SELECT scope.organization_id, scope.project_id INTO organization_id, project_id
        FROM interaction_gateway_turn_watch_work_scopes AS scope
        WHERE scope.due_at <= clock_timestamp() ORDER BY scope.due_at, scope.turn_id LIMIT 1;
    ELSIF requested_kind = 'TEAM_OPERATION' THEN
        SELECT scope.organization_id, scope.project_id INTO organization_id, project_id
        FROM interaction_gateway_team_operation_work_scopes AS scope
        WHERE scope.due_at <= clock_timestamp() ORDER BY scope.due_at, scope.operation_id LIMIT 1;
    ELSIF requested_kind = 'WORKSPACE_MAPPING' THEN
        SELECT scope.organization_id, scope.project_id INTO organization_id, project_id
        FROM interaction_gateway_workspace_mapping_work_scopes AS scope
        WHERE scope.due_at <= clock_timestamp() ORDER BY scope.due_at, scope.operation_id LIMIT 1;
    ELSIF requested_kind = 'AGENT_BOT_IDENTITY' THEN
        SELECT scope.organization_id, scope.project_id INTO organization_id, project_id
        FROM interaction_gateway_agent_bot_work_scopes AS scope
        WHERE scope.due_at <= clock_timestamp() ORDER BY scope.due_at, scope.operation_id LIMIT 1;
    ELSE
        RAISE EXCEPTION 'runtime work kind is invalid' USING ERRCODE = '22023';
    END IF;
END
$function$;
-- +goose StatementEnd

ALTER TABLE interaction_gateway_agent_bot_identities ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_identities FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_bindings FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_ownership ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_ownership FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_watermarks ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_watermarks FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_selectors ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_selectors FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_catalog_cursors ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_catalog_cursors FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_agent_bot_operations FORCE ROW LEVEL SECURITY;

CREATE POLICY interaction_gateway_agent_bot_identity_scope ON interaction_gateway_agent_bot_identities
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_agent_bot_binding_scope ON interaction_gateway_agent_bot_bindings
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_agent_bot_ownership_scope ON interaction_gateway_agent_bot_ownership
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_agent_bot_watermark_scope ON interaction_gateway_agent_bot_watermarks
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_agent_bot_selector_scope ON interaction_gateway_agent_bot_selectors
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_agent_bot_cursor_scope ON interaction_gateway_agent_bot_catalog_cursors
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_agent_bot_operation_scope ON interaction_gateway_agent_bot_operations
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));

REVOKE ALL ON interaction_gateway_agent_bot_metadata, interaction_gateway_agent_bot_identities,
    interaction_gateway_agent_bot_bindings, interaction_gateway_agent_bot_ownership,
    interaction_gateway_agent_bot_watermarks,
    interaction_gateway_agent_bot_selectors, interaction_gateway_agent_bot_catalog_cursors,
    interaction_gateway_agent_bot_operations, interaction_gateway_agent_bot_work_scopes FROM PUBLIC;
REVOKE ALL ON interaction_gateway_agent_bot_work_scopes FROM interaction_gateway_runtime;
GRANT SELECT ON interaction_gateway_agent_bot_metadata TO interaction_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON interaction_gateway_agent_bot_identities,
    interaction_gateway_agent_bot_bindings, interaction_gateway_agent_bot_ownership,
    interaction_gateway_agent_bot_watermarks,
    interaction_gateway_agent_bot_selectors, interaction_gateway_agent_bot_catalog_cursors,
    interaction_gateway_agent_bot_operations TO interaction_gateway_runtime;

-- +goose Down
-- Forward-only: provider checkpoints, consumed receipts и monotonic generations не удаляются.
SELECT 1;
