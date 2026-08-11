-- +goose Up
RESET ROLE;
SET ROLE interaction_gateway_owner;
ALTER TABLE interaction_gateway_team_metadata
    DROP CONSTRAINT interaction_gateway_team_metadata_schema_version_check;
UPDATE interaction_gateway_team_metadata SET schema_version = 2 WHERE singleton;
ALTER TABLE interaction_gateway_team_metadata
    ADD CONSTRAINT interaction_gateway_team_metadata_schema_version_check CHECK (schema_version = 2);

CREATE TABLE interaction_gateway_workspace_mapping_operations (
    operation_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('bind', 'relink', 'unlink')),
    idempotency_key uuid NOT NULL,
    request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[0-9a-f]{64}$'),
    mapping_id uuid,
    expected_mapping_version bigint NOT NULL CHECK (expected_mapping_version >= 0),
    expected_mapping_generation bigint NOT NULL CHECK (expected_mapping_generation >= 0),
    display_name text NOT NULL DEFAULT '' CHECK (length(display_name) <= 256),
    selector_id uuid NOT NULL,
    provider_team_id text NOT NULL CHECK (length(provider_team_id) BETWEEN 1 AND 64),
    provider_status text NOT NULL CHECK (provider_status IN ('ACTIVE', 'DELETED')),
    provider_snapshot_sha256 text NOT NULL CHECK (provider_snapshot_sha256 ~ '^[0-9a-f]{64}$'),
    provider_created_at timestamptz NOT NULL,
    provider_updated_at timestamptz NOT NULL,
    provider_observed_at timestamptz NOT NULL,
    effect_generation bigint CHECK (effect_generation IS NULL OR effect_generation > 0),
    receipt_id uuid,
    state text NOT NULL CHECK (state IN ('PENDING', 'AMBIGUOUS', 'BOUND', 'UNLINKED', 'REPAIR_REQUIRED')),
    result_mapping_id uuid,
    result_mapping_version bigint CHECK (result_mapping_version IS NULL OR result_mapping_version > 0),
    result_mapping_generation bigint CHECK (result_mapping_generation IS NULL OR result_mapping_generation > 0),
    result_provider_effect_version bigint CHECK (result_provider_effect_version IS NULL OR result_provider_effect_version > 0),
    result_provider_effect_generation bigint CHECK (result_provider_effect_generation IS NULL OR result_provider_effect_generation > 0),
    result_provider_observed_at timestamptz,
    result_updated_at timestamptz,
    failure_code text NOT NULL DEFAULT '' CHECK (failure_code ~ '^[A-Z0-9_]{0,64}$'),
    fence bigint NOT NULL DEFAULT 1 CHECK (fence > 0),
    lease_owner text NOT NULL DEFAULT '' CHECK (length(lease_owner) <= 128),
    lease_token_sha256 text NOT NULL DEFAULT '' CHECK (lease_token_sha256 = '' OR lease_token_sha256 ~ '^[0-9a-f]{64}$'),
    lease_expires_at timestamptz,
    retry_not_before timestamptz NOT NULL DEFAULT clock_timestamp(),
    recovery_deadline timestamptz NOT NULL,
    create_operation_id uuid REFERENCES interaction_gateway_team_operations(operation_id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, project_id, actor_id, action, idempotency_key),
    CHECK ((action = 'bind' AND mapping_id IS NULL AND expected_mapping_version = 0
            AND expected_mapping_generation = 0 AND display_name <> '') OR
           (action IN ('relink', 'unlink') AND mapping_id IS NOT NULL
            AND expected_mapping_version > 0 AND expected_mapping_generation > 0 AND display_name = '')),
    CHECK (state NOT IN ('BOUND', 'UNLINKED') OR
           (result_mapping_id IS NOT NULL AND result_mapping_version IS NOT NULL AND result_mapping_generation IS NOT NULL
            AND result_provider_effect_version IS NOT NULL
            AND result_provider_effect_generation IS NOT NULL
            AND result_provider_observed_at IS NOT NULL AND result_updated_at IS NOT NULL)),
    CHECK ((state = 'PENDING' AND lease_token_sha256 <> '' AND lease_expires_at IS NOT NULL) OR
           (state = 'AMBIGUOUS' AND ((lease_token_sha256 = '' AND lease_expires_at IS NULL) OR
                                     (lease_token_sha256 <> '' AND lease_expires_at IS NOT NULL))) OR
           (state NOT IN ('PENDING', 'AMBIGUOUS') AND lease_token_sha256 = '' AND lease_expires_at IS NULL))
);
CREATE INDEX interaction_gateway_workspace_mapping_operation_due_idx
    ON interaction_gateway_workspace_mapping_operations(retry_not_before, created_at)
    WHERE state IN ('PENDING', 'AMBIGUOUS');

CREATE TABLE interaction_gateway_workspace_mapping_work_scopes (
    operation_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    due_at timestamptz NOT NULL
);

-- Authoritative runtime admission — атомарная joined projection owner mapping
-- и fresh Mattermost Team/channel readback. Manifest задаёт лишь template_key.
CREATE TABLE interaction_gateway_mattermost_runtime_routes (
    template_key uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    mapping_owner_actor_id uuid NOT NULL,
    mapping_id uuid NOT NULL,
    mapping_version bigint NOT NULL CHECK (mapping_version > 0),
    mapping_generation bigint NOT NULL CHECK (mapping_generation > 0),
    mapping_digest_sha256 text NOT NULL CHECK (mapping_digest_sha256 ~ '^[0-9a-f]{64}$'),
    provider_team_id text NOT NULL CHECK (length(provider_team_id) BETWEEN 1 AND 64),
    provider_snapshot_sha256 text NOT NULL CHECK (provider_snapshot_sha256 ~ '^[0-9a-f]{64}$'),
    chat_id uuid NOT NULL,
    role_id uuid NOT NULL,
    locale text NOT NULL CHECK (length(locale) BETWEEN 2 AND 32),
    bot_stable_key text NOT NULL CHECK (length(bot_stable_key) BETWEEN 1 AND 64),
    channel_id text NOT NULL CHECK (length(channel_id) BETWEEN 1 AND 64),
    session_id uuid,
    owner_delivery boolean NOT NULL,
    route_digest_sha256 text NOT NULL CHECK (route_digest_sha256 ~ '^[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (provider_team_id, channel_id),
    UNIQUE (organization_id, project_id, mapping_id, mapping_generation, template_key)
);
CREATE UNIQUE INDEX interaction_gateway_mattermost_owner_delivery_idx
    ON interaction_gateway_mattermost_runtime_routes(organization_id, project_id)
    WHERE owner_delivery;

-- High-watermark живёт отдельно от route rows: UNLINKED удаляет executable
-- route, но не позволяет поздней старой generation снова её материализовать.
CREATE TABLE interaction_gateway_mattermost_runtime_checkpoints (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    mapping_id uuid NOT NULL,
    mapping_version bigint NOT NULL CHECK (mapping_version > 0),
    mapping_generation bigint NOT NULL CHECK (mapping_generation > 0),
    mapping_state text NOT NULL CHECK (mapping_state IN ('BOUND', 'UNLINKED')),
    mapping_digest_sha256 text NOT NULL CHECK (mapping_digest_sha256 ~ '^[0-9a-f]{64}$'),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, project_id)
);

-- Минимальный locator index не содержит mapping/provider snapshot и доступен
-- runtime только через SECURITY DEFINER scope resolver.
CREATE TABLE interaction_gateway_mattermost_runtime_route_scopes (
    template_key uuid PRIMARY KEY,
    provider_team_id text NOT NULL,
    channel_id text NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    UNIQUE (provider_team_id, channel_id)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_sync_mattermost_runtime_route_scope()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
BEGIN
    IF TG_OP = 'DELETE' THEN
        DELETE FROM interaction_gateway_mattermost_runtime_route_scopes WHERE template_key = OLD.template_key;
        RETURN OLD;
    END IF;
    INSERT INTO interaction_gateway_mattermost_runtime_route_scopes(
        template_key, provider_team_id, channel_id, organization_id, project_id
    ) VALUES (NEW.template_key, NEW.provider_team_id, NEW.channel_id, NEW.organization_id, NEW.project_id)
    ON CONFLICT (template_key) DO UPDATE SET provider_team_id = EXCLUDED.provider_team_id,
        channel_id = EXCLUDED.channel_id, organization_id = EXCLUDED.organization_id,
        project_id = EXCLUDED.project_id;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd
CREATE TRIGGER interaction_gateway_mattermost_runtime_route_scope_trigger
AFTER INSERT OR UPDATE OR DELETE ON interaction_gateway_mattermost_runtime_routes
FOR EACH ROW EXECUTE FUNCTION interaction_gateway_sync_mattermost_runtime_route_scope();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_mattermost_runtime_route_scope(
    requested_team_id text, requested_channel_id text,
    OUT organization_id uuid, OUT project_id uuid
) RETURNS record LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'interaction_gateway_runtime', 'member') OR NOT EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals AS principal
        JOIN interaction_gateway_runtime_credential_fence AS fence ON fence.singleton
        WHERE principal.principal_name::text = session_user AND principal.status = 'CURRENT'
          AND principal.generation = fence.served_generation
    ) THEN
        RAISE EXCEPTION 'runtime identity is not active' USING ERRCODE = '28000';
    END IF;
    SELECT route.organization_id, route.project_id INTO organization_id, project_id
    FROM interaction_gateway_mattermost_runtime_route_scopes AS route
    WHERE route.provider_team_id = requested_team_id AND route.channel_id = requested_channel_id;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_sync_workspace_mapping_work_scope()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
BEGIN
    IF NEW.state IN ('PENDING', 'AMBIGUOUS') THEN
        INSERT INTO interaction_gateway_workspace_mapping_work_scopes(operation_id, organization_id, project_id, due_at)
        VALUES (NEW.operation_id, NEW.organization_id, NEW.project_id,
            CASE WHEN NEW.lease_expires_at IS NOT NULL
                THEN GREATEST(NEW.retry_not_before, NEW.lease_expires_at)
                ELSE NEW.retry_not_before END)
        ON CONFLICT (operation_id) DO UPDATE SET due_at = EXCLUDED.due_at;
    ELSE
        DELETE FROM interaction_gateway_workspace_mapping_work_scopes WHERE operation_id = NEW.operation_id;
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd
CREATE TRIGGER interaction_gateway_workspace_mapping_work_scope_trigger
AFTER INSERT OR UPDATE OF state, retry_not_before, lease_expires_at
ON interaction_gateway_workspace_mapping_operations
FOR EACH ROW EXECUTE FUNCTION interaction_gateway_sync_workspace_mapping_work_scope();

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
    ELSE
        RAISE EXCEPTION 'runtime work kind is invalid' USING ERRCODE = '22023';
    END IF;
END
$function$;
-- +goose StatementEnd

ALTER TABLE interaction_gateway_workspace_mapping_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_workspace_mapping_operations FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_mattermost_runtime_routes ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_mattermost_runtime_routes FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_mattermost_runtime_checkpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_mattermost_runtime_checkpoints FORCE ROW LEVEL SECURITY;
CREATE POLICY interaction_gateway_workspace_mapping_runtime_scope
ON interaction_gateway_workspace_mapping_operations
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_mattermost_runtime_route_scope
ON interaction_gateway_mattermost_runtime_routes
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_mattermost_runtime_checkpoint_scope
ON interaction_gateway_mattermost_runtime_checkpoints
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));

REVOKE ALL ON interaction_gateway_workspace_mapping_operations,
    interaction_gateway_workspace_mapping_work_scopes,
    interaction_gateway_mattermost_runtime_routes,
    interaction_gateway_mattermost_runtime_checkpoints,
    interaction_gateway_mattermost_runtime_route_scopes FROM PUBLIC;
REVOKE ALL ON interaction_gateway_workspace_mapping_work_scopes FROM interaction_gateway_runtime;
REVOKE ALL ON interaction_gateway_mattermost_runtime_route_scopes FROM interaction_gateway_runtime;
REVOKE ALL ON FUNCTION interaction_gateway_mattermost_runtime_route_scope(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION interaction_gateway_mattermost_runtime_route_scope(text, text) TO interaction_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON interaction_gateway_workspace_mapping_operations,
    interaction_gateway_mattermost_runtime_routes,
    interaction_gateway_mattermost_runtime_checkpoints TO interaction_gateway_runtime;
GRANT DELETE ON interaction_gateway_mattermost_runtime_routes TO interaction_gateway_runtime;

-- +goose Down
-- Forward-only: mapping receipts, provider checkpoints и monotonic watermark не удаляются.
SELECT 1;
