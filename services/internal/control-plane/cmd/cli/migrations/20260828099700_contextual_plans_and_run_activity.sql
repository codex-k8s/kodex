-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.assistant_conversations
    ADD COLUMN title_source text NOT NULL DEFAULT 'SERVER_DEFAULT'
        CHECK (title_source IN ('SERVER_DEFAULT', 'AGENT_PROPOSED', 'USER_EDITED')),
    ADD COLUMN title_revision bigint NOT NULL DEFAULT 1 CHECK (title_revision > 0),
    ADD COLUMN context_route text NOT NULL DEFAULT '' CHECK (char_length(context_route) <= 500),
    ADD COLUMN context_entity_kind text NOT NULL DEFAULT '' CHECK (char_length(context_entity_kind) <= 80),
    ADD COLUMN context_entity_ref text NOT NULL DEFAULT '' CHECK (char_length(context_entity_ref) <= 96),
    ADD COLUMN context_entity_name text NOT NULL DEFAULT '' CHECK (char_length(context_entity_name) <= 300),
    ADD COLUMN context_entity_version bigint CHECK (context_entity_version > 0),
    ADD COLUMN allowed_operations text[] NOT NULL DEFAULT '{}';

ALTER TABLE control_plane.assistant_plans
    DROP CONSTRAINT assistant_plans_state_check;

UPDATE control_plane.assistant_plans SET state = 'DRAFT' WHERE state = 'PROPOSED';
UPDATE control_plane.assistant_plans SET state = 'INVALID' WHERE state IN ('APPLYING', 'FAILED');

ALTER TABLE control_plane.assistant_plans
    ADD CONSTRAINT assistant_plans_state_check
        CHECK (state IN ('DRAFT', 'VALID', 'INVALID', 'STALE', 'APPLIED', 'REJECTED')),
    ADD COLUMN current_revision bigint NOT NULL DEFAULT 1 CHECK (current_revision > 0),
    ADD COLUMN validated_revision bigint CHECK (validated_revision > 0),
    ADD COLUMN content_digest text NOT NULL DEFAULT repeat('0', 64)
        CHECK (content_digest ~ '^[a-f0-9]{64}$'),
    ADD COLUMN validation_problems text[] NOT NULL DEFAULT '{}',
    ADD COLUMN validated_at timestamptz,
    ADD CONSTRAINT assistant_plan_validation_revision_check
        CHECK (validated_revision IS NULL OR validated_revision <= current_revision);

CREATE TABLE control_plane.assistant_plan_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    plan_id uuid NOT NULL REFERENCES control_plane.assistant_plans(id),
    revision bigint NOT NULL CHECK (revision > 0),
    summary text NOT NULL CHECK (char_length(summary) BETWEEN 1 AND 2000),
    operations jsonb NOT NULL
        CHECK (jsonb_typeof(operations) = 'array' AND jsonb_array_length(operations) BETWEEN 1 AND 32 AND octet_length(operations::text) <= 262144),
    content_digest text NOT NULL CHECK (content_digest ~ '^[a-f0-9]{64}$'),
    created_by_kind text NOT NULL CHECK (created_by_kind IN ('USER', 'SYSTEM_ASSISTANT')),
    created_by_ref text NOT NULL CHECK (char_length(created_by_ref) BETWEEN 1 AND 96),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (plan_id, revision)
);

CREATE TABLE control_plane.assistant_plan_receipts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    plan_id uuid NOT NULL REFERENCES control_plane.assistant_plans(id),
    plan_revision bigint NOT NULL CHECK (plan_revision > 0),
    outcome text NOT NULL CHECK (outcome IN ('APPLIED', 'CONFLICT', 'REJECTED')),
    operation_receipts jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(operation_receipts) = 'array' AND octet_length(operation_receipts::text) <= 131072),
    conflict_diff jsonb NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(conflict_diff) = 'array' AND octet_length(conflict_diff::text) <= 131072),
    audit_refs text[] NOT NULL DEFAULT '{}',
    created_resource_refs text[] NOT NULL DEFAULT '{}',
    applied_by uuid REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX assistant_plan_revisions_plan_recent
    ON control_plane.assistant_plan_revisions (plan_id, revision DESC);
CREATE INDEX assistant_plan_receipts_plan_recent
    ON control_plane.assistant_plan_receipts (plan_id, created_at DESC);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.reject_immutable_assistant_record()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'assistant plan revision and receipt records are immutable';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER protect_assistant_plan_revisions
BEFORE UPDATE OR DELETE ON control_plane.assistant_plan_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.reject_immutable_assistant_record();

CREATE TRIGGER protect_assistant_plan_receipts
BEFORE UPDATE OR DELETE ON control_plane.assistant_plan_receipts
FOR EACH ROW EXECUTE FUNCTION control_plane.reject_immutable_assistant_record();

ALTER TABLE control_plane.runs
    ADD COLUMN title_source text NOT NULL DEFAULT 'SERVER_DEFAULT'
        CHECK (title_source IN ('SERVER_DEFAULT', 'AGENT_PROPOSED', 'USER_EDITED')),
    ADD COLUMN presentation_metadata jsonb NOT NULL DEFAULT '{}'::jsonb
        CHECK (jsonb_typeof(presentation_metadata) = 'object' AND octet_length(presentation_metadata::text) <= 4096);

ALTER TABLE control_plane.run_nodes
    DROP CONSTRAINT run_nodes_state_check,
    ADD CONSTRAINT run_nodes_state_check
        CHECK (state IN ('PLANNED', 'QUEUED', 'RUNNING', 'WAITING', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'SKIPPED')),
    ADD COLUMN materialization_state text NOT NULL DEFAULT 'MATERIALIZED'
        CHECK (materialization_state IN ('PLANNED', 'MATERIALIZED')),
    ADD CONSTRAINT run_nodes_materialization_check
        CHECK ((materialization_state = 'PLANNED' AND state IN ('PLANNED', 'CANCELLED', 'SKIPPED') AND turn_id IS NULL) OR materialization_state = 'MATERIALIZED');

ALTER TABLE control_plane.run_events
    DROP CONSTRAINT run_events_type_check,
    ADD CONSTRAINT run_events_type_check
        CHECK (type IN ('RUN_CREATED', 'RUN_STATE_CHANGED', 'RUN_METADATA_UPDATED', 'NODE_ADDED', 'NODE_STATE_CHANGED', 'EDGE_ADDED', 'TURN_QUEUED', 'TURN_STARTED', 'TURN_PROGRESS', 'TURN_COMPLETED', 'DELEGATION_CREATED', 'CALLBACK_DELIVERED', 'OWNER_GATE_OPENED', 'OWNER_GATE_RESOLVED', 'ARTIFACT_AVAILABLE', 'INCIDENT_LINKED', 'TOOL_CALL_RECORDED', 'PLAN_UPDATED')),
    ADD COLUMN actor_kind text NOT NULL DEFAULT 'PLATFORM'
        CHECK (actor_kind IN ('USER', 'AGENT', 'SYSTEM_ASSISTANT', 'PLATFORM', 'INTEGRATION')),
    ADD COLUMN actor_ref text NOT NULL DEFAULT '' CHECK (char_length(actor_ref) <= 96),
    ADD COLUMN actor_name text NOT NULL DEFAULT '' CHECK (char_length(actor_name) <= 300),
    ADD COLUMN message_kind text NOT NULL DEFAULT 'STATE'
        CHECK (message_kind IN ('STATE', 'USER_MESSAGE', 'ASSISTANT_MESSAGE', 'INTERMEDIATE_MESSAGE', 'FINAL_MESSAGE', 'TOOL_CALL', 'PLAN_UPDATE', 'ARTIFACT', 'INCIDENT', 'OWNER_GATE')),
    ADD COLUMN tool_call jsonb
        CHECK (tool_call IS NULL OR (jsonb_typeof(tool_call) = 'object' AND octet_length(tool_call::text) <= 16384));

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

ALTER TABLE control_plane.run_events
    DROP COLUMN tool_call,
    DROP COLUMN message_kind,
    DROP COLUMN actor_name,
    DROP COLUMN actor_ref,
    DROP COLUMN actor_kind,
    DROP CONSTRAINT run_events_type_check,
    ADD CONSTRAINT run_events_type_check
        CHECK (type IN ('RUN_CREATED', 'RUN_STATE_CHANGED', 'NODE_ADDED', 'NODE_STATE_CHANGED', 'EDGE_ADDED', 'TURN_QUEUED', 'TURN_STARTED', 'TURN_PROGRESS', 'TURN_COMPLETED', 'DELEGATION_CREATED', 'CALLBACK_DELIVERED', 'OWNER_GATE_OPENED', 'OWNER_GATE_RESOLVED', 'ARTIFACT_AVAILABLE', 'INCIDENT_LINKED'));

ALTER TABLE control_plane.run_nodes
    DROP CONSTRAINT run_nodes_materialization_check,
    DROP COLUMN materialization_state,
    DROP CONSTRAINT run_nodes_state_check,
    ADD CONSTRAINT run_nodes_state_check
        CHECK (state IN ('QUEUED', 'RUNNING', 'WAITING', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'SKIPPED'));

ALTER TABLE control_plane.runs DROP COLUMN presentation_metadata, DROP COLUMN title_source;

DROP TRIGGER protect_assistant_plan_receipts ON control_plane.assistant_plan_receipts;
DROP TRIGGER protect_assistant_plan_revisions ON control_plane.assistant_plan_revisions;
DROP FUNCTION control_plane.reject_immutable_assistant_record();
DROP TABLE control_plane.assistant_plan_receipts;
DROP TABLE control_plane.assistant_plan_revisions;

UPDATE control_plane.assistant_plans SET state='PROPOSED' WHERE state IN ('DRAFT','VALID','INVALID','STALE');

ALTER TABLE control_plane.assistant_plans
    DROP CONSTRAINT assistant_plan_validation_revision_check,
    DROP COLUMN validated_at,
    DROP COLUMN validation_problems,
    DROP COLUMN content_digest,
    DROP COLUMN validated_revision,
    DROP COLUMN current_revision,
    DROP CONSTRAINT assistant_plans_state_check,
    ADD CONSTRAINT assistant_plans_state_check
        CHECK (state IN ('PROPOSED', 'APPLYING', 'APPLIED', 'REJECTED', 'FAILED'));

ALTER TABLE control_plane.assistant_conversations
    DROP COLUMN allowed_operations,
    DROP COLUMN context_entity_version,
    DROP COLUMN context_entity_name,
    DROP COLUMN context_entity_ref,
    DROP COLUMN context_entity_kind,
    DROP COLUMN context_route,
    DROP COLUMN title_revision,
    DROP COLUMN title_source;

RESET ROLE;
