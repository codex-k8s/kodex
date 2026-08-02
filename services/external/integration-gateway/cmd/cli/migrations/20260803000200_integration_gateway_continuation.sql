-- +goose Up
ALTER TABLE integration_gateway.execution_attempts
    ADD COLUMN provider_dispatched_at timestamptz;

CREATE TABLE integration_gateway.continuation_effects (
    invocation_id text PRIMARY KEY REFERENCES integration_gateway.invocations(invocation_id),
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    action text NOT NULL CHECK (action IN ('NONE', 'SUSPEND', 'APPROVE', 'REJECT', 'CANCEL', 'EXPIRE', 'BEGIN', 'SUCCEED', 'FAIL')),
    desired_action text NOT NULL CHECK (desired_action IN ('NONE', 'APPROVE', 'REJECT', 'CANCEL', 'EXPIRE', 'BEGIN', 'SUCCEED', 'FAIL')),
    continuation_id text NOT NULL DEFAULT '',
    continuation_version bigint NOT NULL DEFAULT 0 CHECK (continuation_version >= 0),
    continuation_fence bigint NOT NULL DEFAULT 0 CHECK (continuation_fence >= 0),
    approval_state text NOT NULL DEFAULT '',
    execution_state text NOT NULL DEFAULT '',
    continuation_state text NOT NULL DEFAULT '',
    application_grant_expires_at timestamptz NOT NULL,
    available_at timestamptz NOT NULL,
    lease_id text NOT NULL DEFAULT '',
    lease_fence bigint NOT NULL DEFAULT 0 CHECK (lease_fence >= 0),
    lease_expires_at timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    updated_at timestamptz NOT NULL
);
CREATE INDEX continuation_effects_claim_idx
    ON integration_gateway.continuation_effects(action, available_at, invocation_id);

CREATE TABLE integration_gateway.continuation_work_scopes (
    invocation_id text PRIMARY KEY,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    available_at timestamptz NOT NULL,
    application_grant_expires_at timestamptz NOT NULL,
    attempts integer NOT NULL CHECK (attempts >= 0)
);
CREATE INDEX continuation_work_scopes_order_idx
    ON integration_gateway.continuation_work_scopes(available_at, invocation_id);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.result_reference_digest(
    requested_invocation_id text,
    requested_attempt_id text,
    requested_status text
) RETURNS text
LANGUAGE plpgsql
IMMUTABLE
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway_extensions
AS $function$
BEGIN
    IF requested_invocation_id !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
       OR requested_attempt_id !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
       OR requested_status NOT IN ('SUCCEEDED', 'FAILED', 'UNKNOWN') THEN
        RAISE EXCEPTION 'result reference digest input is invalid' USING ERRCODE = '22023';
    END IF;
    RETURN encode(
        integration_gateway_extensions.digest(
            convert_to(
                'integration-gateway://invocations/' || requested_invocation_id
                || '/results/' || requested_attempt_id,
                'UTF8'
            ) || decode('00', 'hex') || convert_to(requested_status, 'UTF8'),
            'sha256'
        ),
        'hex'
    );
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.sync_execution_work_scope()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
DECLARE
    eligible boolean;
BEGIN
    SELECT CASE
        WHEN NEW.status = 'APPROVED' THEN
            effect.action = 'NONE' AND effect.approval_state = 'APPROVED'
            AND effect.execution_state = 'NOT_STARTED'
            AND effect.continuation_state = 'SUSPENDED'
            AND effect.application_grant_expires_at > clock_timestamp()
        WHEN NEW.status = 'EXECUTING' THEN
            effect.action = 'NONE' AND effect.approval_state = 'APPROVED'
            AND effect.execution_state = 'EXECUTING'
            AND effect.continuation_state = 'SUSPENDED'
            AND effect.application_grant_expires_at > clock_timestamp()
            AND EXISTS (
                SELECT 1 FROM integration_gateway.execution_attempts AS attempt
                 WHERE attempt.invocation_id = NEW.invocation_id
                   AND attempt.finished_at IS NULL
                   AND attempt.provider_dispatched_at IS NULL
            )
        ELSE false
    END INTO eligible
      FROM integration_gateway.continuation_effects AS effect
     WHERE effect.invocation_id = NEW.invocation_id;
    IF coalesce(eligible, false) AND NEW.expires_at > clock_timestamp() THEN
        INSERT INTO integration_gateway.execution_work_scopes (
            invocation_id, tenant_id, project_id, available_at
        ) VALUES (NEW.invocation_id, NEW.tenant_id, NEW.project_id, NEW.updated_at)
        ON CONFLICT (invocation_id) DO UPDATE SET
            tenant_id = EXCLUDED.tenant_id,
            project_id = EXCLUDED.project_id,
            available_at = EXCLUDED.available_at;
    ELSE
        DELETE FROM integration_gateway.execution_work_scopes
         WHERE invocation_id = NEW.invocation_id;
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd

-- Непосредственный helper нужен effect-trigger: trigger-function нельзя вызвать
-- как обычную функцию.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.sync_execution_work_scope_for(selected_invocation_id text)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
DECLARE
    invocation integration_gateway.invocations%ROWTYPE;
    effect integration_gateway.continuation_effects%ROWTYPE;
    eligible boolean := false;
BEGIN
    SELECT * INTO invocation FROM integration_gateway.invocations
     WHERE invocation_id = selected_invocation_id;
    SELECT * INTO effect FROM integration_gateway.continuation_effects
     WHERE invocation_id = selected_invocation_id;
    IF FOUND THEN
        eligible := invocation.expires_at > clock_timestamp()
            AND effect.action = 'NONE'
            AND effect.application_grant_expires_at > clock_timestamp()
            AND effect.approval_state = 'APPROVED'
            AND effect.continuation_state = 'SUSPENDED'
            AND (
                (invocation.status = 'APPROVED' AND effect.execution_state = 'NOT_STARTED')
                OR
                (invocation.status = 'EXECUTING' AND effect.execution_state = 'EXECUTING'
                    AND EXISTS (
                        SELECT 1 FROM integration_gateway.execution_attempts AS attempt
                         WHERE attempt.invocation_id = selected_invocation_id
                           AND attempt.finished_at IS NULL
                           AND attempt.provider_dispatched_at IS NULL
                    ))
            );
    END IF;
    IF eligible THEN
        INSERT INTO integration_gateway.execution_work_scopes (
            invocation_id, tenant_id, project_id, available_at
        ) VALUES (invocation.invocation_id, invocation.tenant_id, invocation.project_id, invocation.updated_at)
        ON CONFLICT (invocation_id) DO UPDATE SET
            tenant_id = EXCLUDED.tenant_id,
            project_id = EXCLUDED.project_id,
            available_at = EXCLUDED.available_at;
    ELSE
        DELETE FROM integration_gateway.execution_work_scopes
         WHERE invocation_id = selected_invocation_id;
    END IF;
END
$function$;
-- +goose StatementEnd

-- Helper выше должен существовать до trigger-function в PostgreSQL.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.sync_continuation_work_scope()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
BEGIN
    IF NEW.action <> 'NONE' AND NEW.application_grant_expires_at > clock_timestamp() THEN
        INSERT INTO integration_gateway.continuation_work_scopes (
            invocation_id, tenant_id, project_id, available_at,
            application_grant_expires_at, attempts
        ) VALUES (
            NEW.invocation_id, NEW.tenant_id, NEW.project_id,
            greatest(NEW.available_at, coalesce(NEW.lease_expires_at, NEW.available_at)),
            NEW.application_grant_expires_at, NEW.attempts
        )
        ON CONFLICT (invocation_id) DO UPDATE SET
            tenant_id = EXCLUDED.tenant_id,
            project_id = EXCLUDED.project_id,
            available_at = EXCLUDED.available_at,
            application_grant_expires_at = EXCLUDED.application_grant_expires_at,
            attempts = EXCLUDED.attempts;
    ELSE
        DELETE FROM integration_gateway.continuation_work_scopes
         WHERE invocation_id = NEW.invocation_id;
    END IF;
    PERFORM integration_gateway.sync_execution_work_scope_for(NEW.invocation_id);
    RETURN NEW;
END
$function$;
-- +goose StatementEnd

CREATE TRIGGER continuation_effects_sync_work_scopes
AFTER INSERT OR UPDATE OF action, desired_action, approval_state, execution_state,
    continuation_state, available_at, lease_expires_at, application_grant_expires_at
ON integration_gateway.continuation_effects
FOR EACH ROW EXECUTE FUNCTION integration_gateway.sync_continuation_work_scope();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.next_continuation_scope(
    OUT tenant_id text, OUT project_id text
) RETURNS record
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
BEGIN
    IF NOT pg_has_role(session_user, 'integration_gateway_runtime', 'member') THEN
        RAISE EXCEPTION 'runtime identity is invalid' USING ERRCODE = '28000';
    END IF;
    PERFORM 1 FROM integration_gateway.runtime_principals AS principal
     WHERE principal.principal_name::text = session_user
       AND principal.status IN ('CURRENT', 'NEXT', 'PREVIOUS')
       AND clock_timestamp() >= principal.not_before AND clock_timestamp() < principal.not_after;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime principal is not active' USING ERRCODE = '28000';
    END IF;
    SELECT scope.tenant_id, scope.project_id INTO tenant_id, project_id
      FROM integration_gateway.continuation_work_scopes AS scope
     WHERE scope.available_at <= clock_timestamp()
     ORDER BY scope.available_at, scope.invocation_id
     LIMIT 1;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.continuation_readiness()
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
BEGIN
    IF NOT integration_gateway.runtime_identity_ready() THEN
        RETURN false;
    END IF;
    RETURN NOT EXISTS (
        SELECT 1 FROM integration_gateway.continuation_work_scopes AS scope
         WHERE scope.application_grant_expires_at <= clock_timestamp() + interval '10 seconds'
            OR scope.available_at <= clock_timestamp() - interval '30 seconds'
            OR scope.attempts >= 8
    );
END
$function$;
-- +goose StatementEnd

ALTER TABLE integration_gateway.continuation_effects ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.continuation_effects FORCE ROW LEVEL SECURITY;
CREATE POLICY continuation_effects_runtime_scope ON integration_gateway.continuation_effects
    USING ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()))
    WITH CHECK ((tenant_id, project_id) = (SELECT tenant_id, project_id FROM integration_gateway.runtime_scope()));

REVOKE ALL ON integration_gateway.continuation_work_scopes FROM integration_gateway_runtime;
REVOKE ALL ON FUNCTION integration_gateway.sync_execution_work_scope_for(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION integration_gateway.sync_continuation_work_scope() FROM PUBLIC;
REVOKE ALL ON FUNCTION integration_gateway.next_continuation_scope() FROM PUBLIC;
REVOKE ALL ON FUNCTION integration_gateway.continuation_readiness() FROM PUBLIC;
REVOKE ALL ON FUNCTION integration_gateway.result_reference_digest(text, text, text) FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON integration_gateway.continuation_effects TO integration_gateway_runtime;
GRANT EXECUTE ON FUNCTION integration_gateway.next_continuation_scope() TO integration_gateway_runtime;
GRANT EXECUTE ON FUNCTION integration_gateway.continuation_readiness() TO integration_gateway_runtime;
GRANT EXECUTE ON FUNCTION integration_gateway.result_reference_digest(text, text, text) TO integration_gateway_runtime;

ALTER TABLE integration_gateway.continuation_effects OWNER TO integration_gateway_owner;
ALTER TABLE integration_gateway.continuation_work_scopes OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.sync_execution_work_scope_for(text) OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.sync_continuation_work_scope() OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.next_continuation_scope() OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.continuation_readiness() OWNER TO integration_gateway_owner;
ALTER FUNCTION integration_gateway.result_reference_digest(text, text, text) OWNER TO integration_gateway_owner;

-- +goose Down
-- Forward-only migration: continuation receipts и fences не открываются назад.
SELECT 1;
