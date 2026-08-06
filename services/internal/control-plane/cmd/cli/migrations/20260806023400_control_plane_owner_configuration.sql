-- +goose Up
-- Целевая owner-конфигурация Issue #234 вводится одним forward-only cutover:
-- новые защищённые виды получают специализированный history, а legacy
-- ROLE/PROMPT_PROFILE становятся неизменяемыми version-pinned входами уже
-- существующих графов исполнения.
RESET ROLE;
SET ROLE control_plane_owner;
SET search_path = pg_catalog, control_plane;

ALTER TABLE control_plane.resources
    DROP CONSTRAINT resources_kind_check,
    DROP CONSTRAINT resources_spec_check,
    ADD CONSTRAINT resources_kind_v2_ck CHECK (kind IN (
        'PROJECT', 'TEAM', 'CHAT', 'ROLE', 'PROMPT_PROFILE',
        'CREDENTIAL_BINDING', 'REPOSITORY_WORKSPACE', 'INTEGRATION',
        'RUNTIME_REVISION', 'SESSION', 'TURN', 'PROCESS_RUN', 'SCHEDULE',
        'OWNER_GATE', 'MEMORY_RECORD', 'WORK_CLAIM', 'ARTIFACT',
        'ROLE_IMAGE_RECIPE', 'IMAGE_BUILD', 'IMAGE_ARTIFACT',
        'ROLE_DEFINITION', 'AGENT', 'AGENT_ASSIGNMENT', 'INSTRUCTION_SET',
        'PROVIDER_CONNECTION_REFERENCE', 'PROVIDER_POOL',
        'WORKSPACE_BACKUP', 'WORKSPACE_RESTORE',
        'WORKSPACE_MATTERMOST_MAPPING'
    )),
    ADD CONSTRAINT resources_spec_v2_ck CHECK (
        jsonb_typeof(spec) = 'object'
        AND octet_length(spec::text) <= CASE
            WHEN kind IN ('INSTRUCTION_SET', 'WORKSPACE_BACKUP', 'WORKSPACE_RESTORE')
                THEN 524288
            ELSE 65536
        END
    );

DROP INDEX control_plane.resources_stable_key_uidx;
CREATE UNIQUE INDEX resources_stable_key_v2_uidx
    ON control_plane.resources (
        organization_id, project_id, kind, (spec ->> 'stableKey')
    )
    WHERE kind IN (
        'TEAM', 'CHAT', 'ROLE', 'ROLE_DEFINITION', 'AGENT',
        'INSTRUCTION_SET', 'PROVIDER_CONNECTION_REFERENCE', 'PROVIDER_POOL'
    ) AND state <> 'DELETED';

CREATE UNIQUE INDEX resources_agent_assignment_active_uidx
    ON control_plane.resources (
        organization_id,
        project_id,
        ((spec ->> 'agentId')::uuid),
        ((spec ->> 'workspaceId')::uuid),
        coalesce(spec ->> 'roomId', '')
    )
    WHERE kind = 'AGENT_ASSIGNMENT'
      AND state NOT IN ('ARCHIVED', 'DELETION_PENDING', 'DELETED');

CREATE UNIQUE INDEX resources_workspace_mapping_uidx
    ON control_plane.resources (
        organization_id, project_id, ((spec ->> 'workspaceId')::uuid)
    )
    WHERE kind = 'WORKSPACE_MATTERMOST_MAPPING'
      AND state NOT IN ('ARCHIVED', 'DELETION_PENDING', 'DELETED');

CREATE TABLE control_plane.protected_resource_history (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    resource_id uuid NOT NULL,
    resource_version bigint NOT NULL
        CHECK (resource_version BETWEEN 1 AND 9007199254740991),
    resource_kind text NOT NULL CHECK (resource_kind IN (
        'ROLE_DEFINITION', 'AGENT', 'AGENT_ASSIGNMENT', 'INSTRUCTION_SET',
        'PROVIDER_CONNECTION_REFERENCE', 'PROVIDER_POOL',
        'WORKSPACE_BACKUP', 'WORKSPACE_RESTORE',
        'WORKSPACE_MATTERMOST_MAPPING'
    )),
    owner_actor_id uuid NOT NULL,
    action text NOT NULL CHECK (action ~ '^[a-z][a-z0-9_]{0,63}$'),
    snapshot jsonb NOT NULL CHECK (
        jsonb_typeof(snapshot) = 'object'
        AND octet_length(snapshot::text) <= 786432
    ),
    snapshot_sha256 text NOT NULL CHECK (snapshot_sha256 ~ '^[a-f0-9]{64}$'),
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, project_id, resource_id, resource_version),
    FOREIGN KEY (organization_id, project_id, resource_id)
        REFERENCES control_plane.resources (organization_id, project_id, id)
);
CREATE INDEX protected_resource_history_owner_page_idx
    ON control_plane.protected_resource_history (
        organization_id, project_id, owner_actor_id, resource_id,
        resource_version DESC
    );
ALTER TABLE control_plane.protected_resource_history OWNER TO control_plane_owner;
ALTER TABLE control_plane.protected_resource_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.protected_resource_history FORCE ROW LEVEL SECURITY;
CREATE POLICY protected_resource_history_runtime_scope
    ON control_plane.protected_resource_history
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    );
REVOKE ALL ON control_plane.protected_resource_history FROM PUBLIC;
GRANT SELECT, INSERT ON control_plane.protected_resource_history TO control_plane_runtime;

ALTER TABLE control_plane.runtime_execution_incidents
    ADD COLUMN version bigint NOT NULL DEFAULT 1
        CHECK (version BETWEEN 1 AND 9007199254740991),
    ADD COLUMN state text NOT NULL DEFAULT 'OPEN'
        CHECK (state IN ('OPEN', 'ACKNOWLEDGED', 'RETRYING', 'RELEASED', 'CLOSED')),
    ADD COLUMN action_reason_code text,
    ADD COLUMN updated_at timestamptz;
UPDATE control_plane.runtime_execution_incidents
SET updated_at = occurred_at
WHERE updated_at IS NULL;
ALTER TABLE control_plane.runtime_execution_incidents
    ALTER COLUMN updated_at SET NOT NULL,
    ADD CONSTRAINT runtime_execution_incidents_reason_v2_ck CHECK (
        (state = 'OPEN' AND action_reason_code IS NULL)
        OR (state <> 'OPEN' AND action_reason_code ~ '^[a-z][a-z0-9._-]{0,127}$')
    ),
    ADD CONSTRAINT runtime_execution_incidents_updated_v2_ck
        CHECK (updated_at >= occurred_at);

CREATE TABLE control_plane.runtime_incident_history (
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    incident_id uuid NOT NULL,
    version bigint NOT NULL CHECK (version BETWEEN 1 AND 9007199254740991),
    owner_actor_id uuid NOT NULL,
    state text NOT NULL CHECK (state IN (
        'OPEN', 'ACKNOWLEDGED', 'RETRYING', 'RELEASED', 'CLOSED'
    )),
    action text NOT NULL CHECK (action IN (
        'record', 'acknowledge', 'retry', 'release', 'close'
    )),
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z][a-z0-9._-]{0,127}$'),
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (organization_id, project_id, incident_id, version),
    FOREIGN KEY (incident_id)
        REFERENCES control_plane.runtime_execution_incidents (id)
);
CREATE INDEX runtime_incident_history_owner_page_idx
    ON control_plane.runtime_incident_history (
        organization_id, project_id, owner_actor_id, incident_id, version DESC
    );
INSERT INTO control_plane.runtime_incident_history (
    organization_id, project_id, incident_id, version, owner_actor_id,
    state, action, reason_code, occurred_at
)
SELECT incident.organization_id, incident.project_id, incident.id,
    incident.version, process.owner_actor_id, incident.state,
    'record', 'incident_recorded', incident.occurred_at
FROM control_plane.runtime_execution_incidents AS incident
JOIN control_plane.runtime_executions AS execution
  ON execution.organization_id = incident.organization_id
 AND execution.project_id = incident.project_id
 AND execution.id = incident.execution_id
JOIN control_plane.resources AS process
  ON process.organization_id = execution.organization_id
 AND process.project_id = execution.project_id
 AND process.id = execution.process_id
 AND process.kind = 'PROCESS_RUN';
ALTER TABLE control_plane.runtime_incident_history OWNER TO control_plane_owner;
ALTER TABLE control_plane.runtime_incident_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.runtime_incident_history FORCE ROW LEVEL SECURITY;
CREATE POLICY runtime_incident_history_runtime_scope
    ON control_plane.runtime_incident_history
    FOR ALL TO control_plane_runtime
    USING (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    )
    WITH CHECK (
        organization_id = (control_plane.runtime_scope()).organization_id
        AND project_id = (control_plane.runtime_scope()).project_id
        AND owner_actor_id = (control_plane.runtime_scope()).actor_id
    );
REVOKE ALL ON control_plane.runtime_incident_history FROM PUBLIC;
GRANT SELECT, INSERT ON control_plane.runtime_incident_history TO control_plane_runtime;
GRANT UPDATE ON control_plane.runtime_execution_incidents TO control_plane_runtime;

CREATE FUNCTION control_plane.reject_legacy_configuration_mutation()
RETURNS trigger
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = pg_catalog, control_plane
AS $function$
BEGIN
    RAISE EXCEPTION 'legacy ROLE and PROMPT_PROFILE configuration is immutable'
        USING ERRCODE = '0A000';
END
$function$;
REVOKE ALL ON FUNCTION control_plane.reject_legacy_configuration_mutation() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.reject_legacy_configuration_mutation()
    TO control_plane_runtime;
CREATE TRIGGER resources_legacy_configuration_freeze
    BEFORE INSERT OR UPDATE OR DELETE ON control_plane.resources
    FOR EACH ROW
    WHEN (
        coalesce(NEW.kind, '') IN ('ROLE', 'PROMPT_PROFILE')
        OR coalesce(OLD.kind, '') IN ('ROLE', 'PROMPT_PROFILE')
    )
    EXECUTE FUNCTION control_plane.reject_legacy_configuration_mutation();

UPDATE control_plane.schema_state
SET version = 20260806023400, migrated_at = clock_timestamp()
WHERE singleton = true;
RESET ROLE;

-- +goose Down
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260806023400 is forward-only: protected owner lifecycle and history cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
