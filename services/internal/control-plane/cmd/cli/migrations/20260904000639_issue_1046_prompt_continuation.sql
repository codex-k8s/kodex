-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.managed_configuration_bindings
    DROP CONSTRAINT managed_configuration_bindings_consumer_kind_check;
ALTER TABLE control_plane.managed_configuration_bindings
    ADD CONSTRAINT managed_configuration_bindings_consumer_kind_check
    CHECK (consumer_kind IN ('AGENT', 'AGENT_CONTINUATION', 'WORKFLOW', 'SCHEDULE',
                            'RUNTIME_ENVIRONMENT', 'INTEGRATION_CONNECTION', 'STT_SERVICE'));

ALTER TABLE control_plane.session_turns
    ADD COLUMN expected_prompt_context_digest text NOT NULL DEFAULT ''
    CHECK (expected_prompt_context_digest = '' OR expected_prompt_context_digest ~ '^[a-f0-9]{64}$');

CREATE TABLE control_plane.session_continuation_notices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    session_id uuid NOT NULL REFERENCES control_plane.sessions(id),
    turn_id uuid NOT NULL REFERENCES control_plane.session_turns(id),
    node_id uuid NOT NULL REFERENCES control_plane.run_nodes(id),
    attempt integer NOT NULL CHECK (attempt > 0),
    previous_runtime_revision_id uuid NOT NULL REFERENCES control_plane.runtime_revisions(id),
    current_runtime_revision_id uuid NOT NULL UNIQUE REFERENCES control_plane.runtime_revisions(id),
    template_ref text NOT NULL CHECK (length(template_ref) BETWEEN 8 AND 128),
    template_digest text NOT NULL CHECK (template_digest ~ '^[a-f0-9]{64}$'),
    service_template_revision text NOT NULL CHECK (service_template_revision = 'prompt-service-v2'),
    service_template_digest text NOT NULL CHECK (service_template_digest ~ '^[a-f0-9]{64}$'),
    variable_snapshot_digest text NOT NULL CHECK (variable_snapshot_digest ~ '^[a-f0-9]{64}$'),
    diff_digest text NOT NULL CHECK (diff_digest ~ '^[a-f0-9]{64}$'),
    materialization_digest text NOT NULL CHECK (materialization_digest ~ '^[a-f0-9]{64}$'),
    content text NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 65536),
    safe_snapshot jsonb NOT NULL CHECK (jsonb_typeof(safe_snapshot) = 'object' AND octet_length(safe_snapshot::text) <= 524288),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (session_id, turn_id, node_id, attempt),
    CHECK (previous_runtime_revision_id <> current_runtime_revision_id)
);
CREATE TRIGGER protect_session_continuation_notice BEFORE UPDATE OR DELETE
ON control_plane.session_continuation_notices
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_provider_model_catalog_observation();
RESET ROLE;
