-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.schedule_occurrences
    ADD COLUMN prompt_input_format integer NOT NULL DEFAULT 0 CHECK (prompt_input_format IN (0,1));
ALTER TABLE control_plane.schedule_occurrences
    DROP CONSTRAINT schedule_occurrences_prompt_inputs_check,
    ADD CONSTRAINT schedule_occurrences_prompt_capture_bound CHECK (
        jsonb_typeof(prompt_inputs)='object' AND octet_length(prompt_inputs::text) <=
        CASE prompt_input_format WHEN 0 THEN 65536 ELSE 262144 END);
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_schedule_prompt_capture() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.prompt_input_format IS DISTINCT FROM OLD.prompt_input_format OR
       NEW.prompt_inputs IS DISTINCT FROM OLD.prompt_inputs OR
       NEW.prompt_inputs_digest IS DISTINCT FROM OLD.prompt_inputs_digest THEN
        RAISE EXCEPTION 'schedule prompt capture is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_schedule_prompt_capture BEFORE UPDATE ON control_plane.schedule_occurrences
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_schedule_prompt_capture();

ALTER TABLE control_plane.session_turns
    ADD COLUMN expected_prompt_dependency_digest text NOT NULL DEFAULT ''
    CHECK (expected_prompt_dependency_digest = '' OR expected_prompt_dependency_digest ~ '^[a-f0-9]{64}$');

CREATE TABLE control_plane.prompt_template_scopes (
    revision_id uuid PRIMARY KEY REFERENCES control_plane.managed_configuration_revisions(id),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    target_kind text NOT NULL CHECK (target_kind IN ('AGENT', 'WORKFLOW_STAGE')),
    target_ref text NOT NULL CHECK (length(target_ref) BETWEEN 8 AND 128),
    template_kind text NOT NULL CHECK (template_kind IN ('INSTRUCTIONS', 'CONTINUATION')),
    context_pin jsonb NOT NULL CHECK (jsonb_typeof(context_pin) = 'object' AND octet_length(context_pin::text) <= 16384),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER protect_prompt_template_scope BEFORE UPDATE OR DELETE
ON control_plane.prompt_template_scopes
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_provider_model_catalog_observation();
RESET ROLE;
