-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.integration_approval_scopes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES control_plane.projects(id) ON DELETE CASCADE,
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id) ON DELETE CASCADE,
    grant_id uuid NOT NULL REFERENCES control_plane.integration_grants(id) ON DELETE CASCADE,
    root_run_id uuid NOT NULL REFERENCES control_plane.runs(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL REFERENCES control_plane.agents(id) ON DELETE CASCADE,
    origin_gate_id uuid NOT NULL UNIQUE REFERENCES control_plane.owner_gates(id) ON DELETE CASCADE,
    capability_key text NOT NULL,
    grant_version bigint NOT NULL CHECK (grant_version > 0),
    definition_digest text NOT NULL CHECK (definition_digest ~ '^[a-f0-9]{64}$'),
    input_schema_digest text NOT NULL CHECK (input_schema_digest ~ '^[a-f0-9]{64}$'),
    scope_paths text[] NOT NULL CHECK (cardinality(scope_paths) BETWEEN 1 AND 16),
    scope_values jsonb NOT NULL CHECK (jsonb_typeof(scope_values) = 'array' AND octet_length(scope_values::text) <= 65536),
    scope_digest text NOT NULL CHECK (scope_digest ~ '^[a-f0-9]{64}$'),
    max_effects integer NOT NULL CHECK (max_effects BETWEEN 1 AND 100),
    reserved_effects integer NOT NULL CHECK (reserved_effects BETWEEN 1 AND max_effects),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (expires_at > created_at)
);

CREATE INDEX integration_approval_scopes_reuse_idx
    ON control_plane.integration_approval_scopes
    (organization_id, root_run_id, agent_id, grant_id, scope_digest)
    WHERE revoked_at IS NULL;

ALTER TABLE control_plane.integration_invocations
    DROP CONSTRAINT integration_invocations_approval_check,
    ADD COLUMN approval_scope_id uuid REFERENCES control_plane.integration_approval_scopes(id) ON DELETE SET NULL,
    ADD CONSTRAINT integration_invocations_approval_check CHECK (
        (risk='READ' AND approval_policy='NONE' AND (state<>'WAITING_APPROVAL' OR mailbox_gate_required)) OR
        (risk IN ('WRITE','SENSITIVE','DESTRUCTIVE') AND approval_policy IN ('HUMAN_EACH_EFFECT','HUMAN_SCOPED')) OR
        (risk IN ('WRITE','SENSITIVE','DESTRUCTIVE') AND approval_policy='NONE' AND resource_kind='EMAIL_SENDER'
            AND (state<>'WAITING_APPROVAL' OR mailbox_gate_required))
    ),
    ADD CONSTRAINT integration_invocations_approval_scope_policy_check CHECK (
        approval_scope_id IS NULL OR approval_policy='HUMAN_SCOPED'
    );

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_integration_approval_scope_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF (to_jsonb(NEW) - 'reserved_effects' - 'revoked_at') IS DISTINCT FROM
       (to_jsonb(OLD) - 'reserved_effects' - 'revoked_at')
       OR NEW.reserved_effects < OLD.reserved_effects
       OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at) THEN
        RAISE EXCEPTION 'integration approval scope binding is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER integration_approval_scopes_guard_update
BEFORE UPDATE ON control_plane.integration_approval_scopes
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_integration_approval_scope_update();

-- +goose StatementBegin
CREATE FUNCTION control_plane.revoke_integration_approval_scopes() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'integration_grants' THEN
        IF NEW.enabled IS DISTINCT FROM OLD.enabled OR NEW.version IS DISTINCT FROM OLD.version
           OR NEW.approval_policy IS DISTINCT FROM OLD.approval_policy
           OR NEW.approval_scope_paths IS DISTINCT FROM OLD.approval_scope_paths
           OR NEW.definition_version IS DISTINCT FROM OLD.definition_version
           OR NEW.definition_digest IS DISTINCT FROM OLD.definition_digest
           OR NEW.resource_scope_digest IS DISTINCT FROM OLD.resource_scope_digest THEN
            UPDATE control_plane.integration_approval_scopes
            SET revoked_at=clock_timestamp() WHERE grant_id=OLD.id AND revoked_at IS NULL;
        END IF;
    ELSIF TG_TABLE_NAME = 'integration_connections' THEN
        IF NEW.enabled IS DISTINCT FROM OLD.enabled OR NEW.state IS DISTINCT FROM OLD.state
           OR NEW.definition_version IS DISTINCT FROM OLD.definition_version
           OR NEW.definition_digest IS DISTINCT FROM OLD.definition_digest THEN
            UPDATE control_plane.integration_approval_scopes
            SET revoked_at=clock_timestamp() WHERE connection_id=OLD.id AND revoked_at IS NULL;
        END IF;
    ELSIF TG_TABLE_NAME = 'integration_definitions' THEN
        IF NEW.enabled IS DISTINCT FROM OLD.enabled OR NEW.digest IS DISTINCT FROM OLD.digest
           OR NEW.definition_version IS DISTINCT FROM OLD.definition_version
           OR NEW.adapter_readiness IS DISTINCT FROM OLD.adapter_readiness THEN
            UPDATE control_plane.integration_approval_scopes scope
            SET revoked_at=clock_timestamp()
            WHERE scope.connection_id IN (
                SELECT connection.id FROM control_plane.integration_connections connection
                WHERE connection.definition_key=OLD.stable_key
            ) AND scope.revoked_at IS NULL;
        END IF;
    ELSIF TG_TABLE_NAME = 'runs' THEN
        IF NEW.state IN ('CANCELLING','SUCCEEDED','FAILED','CANCELLED') AND NEW.state IS DISTINCT FROM OLD.state THEN
            UPDATE control_plane.integration_approval_scopes
            SET revoked_at=clock_timestamp() WHERE root_run_id=OLD.id AND revoked_at IS NULL;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER integration_approval_scopes_revoke_grant
AFTER UPDATE ON control_plane.integration_grants
FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_integration_approval_scopes();
CREATE TRIGGER integration_approval_scopes_revoke_connection
AFTER UPDATE ON control_plane.integration_connections
FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_integration_approval_scopes();
CREATE TRIGGER integration_approval_scopes_revoke_definition
AFTER UPDATE ON control_plane.integration_definitions
FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_integration_approval_scopes();
CREATE TRIGGER integration_approval_scopes_revoke_run
AFTER UPDATE OF state ON control_plane.runs
FOR EACH ROW EXECUTE FUNCTION control_plane.revoke_integration_approval_scopes();

GRANT SELECT, INSERT, UPDATE ON control_plane.integration_approval_scopes TO control_plane_runtime;

RESET ROLE;
