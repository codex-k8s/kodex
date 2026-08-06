-- +goose Up
CREATE TABLE interaction_gateway_team_metadata (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    schema_version bigint NOT NULL CHECK (schema_version = 1)
);
INSERT INTO interaction_gateway_team_metadata(singleton, schema_version) VALUES (true, 1);

CREATE TABLE interaction_gateway_team_catalog_selectors (
    selector_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    provider_team_id text NOT NULL CHECK (length(provider_team_id) BETWEEN 1 AND 64),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 256),
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,63}$'),
    status text NOT NULL CHECK (status IN ('ACTIVE', 'DELETED')),
    provider_snapshot_sha256 text NOT NULL CHECK (provider_snapshot_sha256 ~ '^[0-9a-f]{64}$'),
    provider_created_at timestamptz NOT NULL,
    provider_updated_at timestamptz NOT NULL,
    observed_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, project_id, actor_id, provider_team_id),
    CHECK (expires_at > observed_at)
);
CREATE INDEX interaction_gateway_team_selector_expiry_idx
    ON interaction_gateway_team_catalog_selectors(expires_at);

CREATE TABLE interaction_gateway_team_catalog_cursors (
    cursor_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    catalog_offset integer NOT NULL CHECK (catalog_offset BETWEEN 0 AND 10000),
    page_size integer NOT NULL CHECK (page_size BETWEEN 1 AND 100),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX interaction_gateway_team_cursor_expiry_idx
    ON interaction_gateway_team_catalog_cursors(expires_at);

CREATE TABLE interaction_gateway_team_provider_watermarks (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    provider_generation bigint NOT NULL CHECK (provider_generation > 0),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (organization_id, project_id)
);

CREATE TABLE interaction_gateway_team_operations (
    operation_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind = 'CREATE'),
    idempotency_key uuid NOT NULL,
    request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[0-9a-f]{64}$'),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 256),
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,63}$'),
    state text NOT NULL CHECK (state IN ('PENDING', 'EFFECT_PENDING', 'AMBIGUOUS', 'PROVIDER_ACCEPTED', 'REPAIR_REQUIRED')),
    selector_id uuid,
    provider_team_id text NOT NULL DEFAULT '' CHECK (length(provider_team_id) <= 64),
    provider_status text NOT NULL DEFAULT '' CHECK (provider_status IN ('', 'ACTIVE', 'DELETED')),
    provider_snapshot_sha256 text NOT NULL DEFAULT '' CHECK (provider_snapshot_sha256 = '' OR provider_snapshot_sha256 ~ '^[0-9a-f]{64}$'),
    provider_receipt_sha256 text NOT NULL DEFAULT '' CHECK (provider_receipt_sha256 = '' OR provider_receipt_sha256 ~ '^[0-9a-f]{64}$'),
    provider_generation bigint CHECK (provider_generation IS NULL OR provider_generation > 0),
    provider_created_at timestamptz,
    provider_updated_at timestamptz,
    provider_observed_at timestamptz,
    failure_code text NOT NULL DEFAULT '' CHECK (failure_code ~ '^[A-Z0-9_]{0,64}$'),
    fence bigint NOT NULL DEFAULT 0 CHECK (fence >= 0),
    lease_owner text NOT NULL DEFAULT '' CHECK (length(lease_owner) <= 128),
    lease_token_sha256 text NOT NULL DEFAULT '' CHECK (lease_token_sha256 = '' OR lease_token_sha256 ~ '^[0-9a-f]{64}$'),
    lease_expires_at timestamptz,
    effect_started_at timestamptz,
    retry_not_before timestamptz NOT NULL DEFAULT clock_timestamp(),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, project_id, actor_id, kind, idempotency_key),
    UNIQUE (organization_id, project_id, provider_generation),
    CHECK (state <> 'EFFECT_PENDING' OR effect_started_at IS NOT NULL),
    CHECK (state <> 'AMBIGUOUS' OR effect_started_at IS NOT NULL),
    CHECK (state <> 'PROVIDER_ACCEPTED' OR
        (selector_id IS NOT NULL AND provider_team_id <> '' AND provider_status = 'ACTIVE'
         AND provider_snapshot_sha256 <> '' AND provider_receipt_sha256 <> ''
         AND provider_generation IS NOT NULL))
);
CREATE INDEX interaction_gateway_team_operation_due_idx
    ON interaction_gateway_team_operations(retry_not_before, created_at)
    WHERE state IN ('PENDING', 'EFFECT_PENDING', 'AMBIGUOUS');

-- Scope index intentionally carries no provider payload or receipt.
CREATE TABLE interaction_gateway_team_operation_work_scopes (
    operation_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    due_at timestamptz NOT NULL
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_sync_team_operation_work_scope()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
BEGIN
    IF NEW.state IN ('PENDING', 'EFFECT_PENDING', 'AMBIGUOUS') THEN
        INSERT INTO interaction_gateway_team_operation_work_scopes(operation_id, organization_id, project_id, due_at)
        VALUES (NEW.operation_id, NEW.organization_id, NEW.project_id,
            CASE WHEN NEW.lease_expires_at IS NOT NULL
                THEN GREATEST(NEW.retry_not_before, NEW.lease_expires_at)
                ELSE NEW.retry_not_before END)
        ON CONFLICT (operation_id) DO UPDATE SET due_at = EXCLUDED.due_at;
    ELSE
        DELETE FROM interaction_gateway_team_operation_work_scopes WHERE operation_id = NEW.operation_id;
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd
CREATE TRIGGER interaction_gateway_team_operation_work_scope_trigger
AFTER INSERT OR UPDATE OF state, retry_not_before, lease_expires_at ON interaction_gateway_team_operations
FOR EACH ROW EXECUTE FUNCTION interaction_gateway_sync_team_operation_work_scope();

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
    ELSE
        RAISE EXCEPTION 'runtime work kind is invalid' USING ERRCODE = '22023';
    END IF;
END
$function$;
-- +goose StatementEnd

ALTER TABLE interaction_gateway_team_catalog_selectors ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_team_catalog_selectors FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_team_catalog_cursors ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_team_catalog_cursors FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_team_provider_watermarks ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_team_provider_watermarks FORCE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_team_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE interaction_gateway_team_operations FORCE ROW LEVEL SECURITY;

CREATE POLICY interaction_gateway_team_selector_runtime_scope ON interaction_gateway_team_catalog_selectors
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_team_cursor_runtime_scope ON interaction_gateway_team_catalog_cursors
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_team_watermark_runtime_scope ON interaction_gateway_team_provider_watermarks
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));
CREATE POLICY interaction_gateway_team_operation_runtime_scope ON interaction_gateway_team_operations
USING ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()))
WITH CHECK ((organization_id, project_id) = (SELECT organization_id, project_id FROM interaction_gateway_runtime_scope()));

REVOKE ALL ON interaction_gateway_team_metadata, interaction_gateway_team_catalog_selectors,
    interaction_gateway_team_catalog_cursors, interaction_gateway_team_provider_watermarks,
    interaction_gateway_team_operations, interaction_gateway_team_operation_work_scopes
    FROM PUBLIC;
REVOKE ALL ON interaction_gateway_team_operation_work_scopes FROM interaction_gateway_runtime;
GRANT SELECT ON interaction_gateway_team_metadata TO interaction_gateway_runtime;
GRANT SELECT, INSERT, UPDATE ON interaction_gateway_team_catalog_selectors,
    interaction_gateway_team_catalog_cursors, interaction_gateway_team_provider_watermarks,
    interaction_gateway_team_operations TO interaction_gateway_runtime;

-- +goose Down
-- Forward-only: semantic receipts, provider checkpoints и monotonic watermark не удаляются.
SELECT 1;
