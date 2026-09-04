-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.schedule_revisions
    DROP CONSTRAINT schedule_revisions_preset_check,
    DROP CONSTRAINT schedule_revisions_cron_expression_check,
    ADD CONSTRAINT schedule_revisions_preset_check
        CHECK (preset IN ('HOURLY', 'DAILY', 'WEEKDAYS', 'WEEKLY', 'CUSTOM')),
    ADD CONSTRAINT schedule_revisions_cron_expression_check
        CHECK (char_length(cron_expression) BETWEEN 9 AND 128);

ALTER TABLE control_plane.schedules
    DROP CONSTRAINT schedules_preset_check,
    DROP CONSTRAINT schedules_cron_expression_check,
    ADD CONSTRAINT schedules_preset_check
        CHECK (preset IN ('HOURLY', 'DAILY', 'WEEKDAYS', 'WEEKLY', 'CUSTOM')),
    ADD CONSTRAINT schedules_cron_expression_check
        CHECK (char_length(cron_expression) BETWEEN 9 AND 128);

CREATE TABLE control_plane.managed_configuration_sets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^mcfg_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    kind text NOT NULL CHECK (kind IN ('PROMPT_TEMPLATE', 'ROLE_IMAGE', 'INTEGRATION_DEFINITION', 'SYSTEM_STT')),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    managed_by text NOT NULL CHECK (managed_by IN ('UI', 'GIT')),
    source text NOT NULL CHECK (char_length(source) BETWEEN 1 AND 1000),
    source_revision text NOT NULL DEFAULT '' CHECK (char_length(source_revision) <= 256),
    current_revision_id uuid,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE NULLS NOT DISTINCT (organization_id, project_id, kind, name),
    CHECK ((managed_by = 'UI' AND source = 'control-center' AND source_revision = '')
        OR (managed_by = 'GIT' AND source <> 'control-center' AND source_revision <> '')),
    CHECK ((kind = 'SYSTEM_STT' AND project_id IS NULL) OR kind <> 'SYSTEM_STT')
);

CREATE TABLE control_plane.managed_configuration_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^mrev_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    configuration_set_id uuid NOT NULL REFERENCES control_plane.managed_configuration_sets(id),
    revision bigint NOT NULL CHECK (revision > 0),
    state text NOT NULL CHECK (state IN ('DRAFT', 'VALID', 'INVALID', 'PUBLISHED', 'SUPERSEDED')),
    content_format text NOT NULL CHECK (content_format IN ('TEXT', 'JSON', 'YAML', 'TOML')),
    content text NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 262144),
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    validation_diagnostics jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(validation_diagnostics) = 'array'),
    parent_revision_id uuid REFERENCES control_plane.managed_configuration_revisions(id),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    validated_at timestamptz,
    published_at timestamptz,
    UNIQUE (configuration_set_id, revision),
    UNIQUE (configuration_set_id, id),
    CHECK ((state IN ('PUBLISHED', 'SUPERSEDED')) = (published_at IS NOT NULL))
);

ALTER TABLE control_plane.managed_configuration_sets
    ADD CONSTRAINT managed_configuration_sets_current_revision_fk
    FOREIGN KEY (id, current_revision_id)
    REFERENCES control_plane.managed_configuration_revisions(configuration_set_id, id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE control_plane.managed_configuration_bindings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^mcbind_[A-Za-z0-9_-]{8,87}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    configuration_set_id uuid NOT NULL REFERENCES control_plane.managed_configuration_sets(id),
    configuration_revision_id uuid NOT NULL REFERENCES control_plane.managed_configuration_revisions(id),
    configuration_kind text NOT NULL CHECK (configuration_kind IN ('PROMPT_TEMPLATE', 'ROLE_IMAGE', 'INTEGRATION_DEFINITION', 'SYSTEM_STT')),
    consumer_kind text NOT NULL CHECK (consumer_kind IN ('AGENT', 'WORKFLOW', 'SCHEDULE', 'RUNTIME_ENVIRONMENT', 'INTEGRATION_CONNECTION', 'STT_SERVICE')),
    consumer_ref text NOT NULL CHECK (char_length(consumer_ref) BETWEEN 8 AND 96),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    rebound_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, configuration_kind, consumer_kind, consumer_ref)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_managed_configuration_revision()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'managed configuration revision is immutable';
    END IF;
    IF OLD.ref <> NEW.ref OR OLD.organization_id <> NEW.organization_id OR
       OLD.configuration_set_id <> NEW.configuration_set_id OR OLD.revision <> NEW.revision OR
       OLD.content_format <> NEW.content_format OR OLD.content <> NEW.content OR OLD.digest <> NEW.digest OR
       OLD.parent_revision_id IS DISTINCT FROM NEW.parent_revision_id OR
       OLD.created_by <> NEW.created_by OR OLD.created_at <> NEW.created_at OR
       NOT ((OLD.state = 'DRAFT' AND NEW.state IN ('VALID','INVALID')) OR
            (OLD.state = 'INVALID' AND NEW.state IN ('VALID','INVALID')) OR
            (OLD.state = 'VALID' AND NEW.state = 'PUBLISHED') OR
            (OLD.state = 'PUBLISHED' AND NEW.state = 'SUPERSEDED')) THEN
        RAISE EXCEPTION 'managed configuration revision is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER protect_managed_configuration_revision
BEFORE UPDATE OR DELETE ON control_plane.managed_configuration_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_managed_configuration_revision();

CREATE INDEX managed_configuration_history
    ON control_plane.managed_configuration_revisions (configuration_set_id, revision DESC);
CREATE UNIQUE INDEX managed_configuration_one_draft
    ON control_plane.managed_configuration_revisions (configuration_set_id)
    WHERE state IN ('DRAFT', 'VALID');
CREATE INDEX managed_configuration_consumers
    ON control_plane.managed_configuration_bindings (configuration_set_id, configuration_kind, consumer_kind, consumer_ref);

INSERT INTO control_plane.platform_capabilities (stable_key, name, description, risk)
VALUES ('platform.stt.use', 'Системное распознавание речи', 'Использование опубликованной системной STT-конфигурации', 'MEDIUM')
ON CONFLICT (stable_key) DO NOTHING;

UPDATE control_plane.permission_registry
SET resource_kinds = ARRAY['ORGANIZATION','AGENT','WORKFLOW','RUN','SESSION','SCHEDULE']
WHERE permission_key = 'prompt.full.view';

GRANT SELECT, INSERT, UPDATE ON control_plane.managed_configuration_sets TO control_plane_runtime;
GRANT SELECT, INSERT, UPDATE ON control_plane.managed_configuration_revisions TO control_plane_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON control_plane.managed_configuration_bindings TO control_plane_runtime;

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

DELETE FROM control_plane.platform_capabilities WHERE stable_key = 'platform.stt.use';
UPDATE control_plane.permission_registry
SET resource_kinds = ARRAY['RUN','SESSION']
WHERE permission_key = 'prompt.full.view';
DROP TABLE control_plane.managed_configuration_bindings;
ALTER TABLE control_plane.managed_configuration_sets DROP CONSTRAINT managed_configuration_sets_current_revision_fk;
DROP TRIGGER protect_managed_configuration_revision ON control_plane.managed_configuration_revisions;
DROP FUNCTION control_plane.protect_managed_configuration_revision();
DROP TABLE control_plane.managed_configuration_revisions;
DROP TABLE control_plane.managed_configuration_sets;

DELETE FROM control_plane.schedule_occurrences
WHERE schedule_id IN (SELECT id FROM control_plane.schedules WHERE preset = 'CUSTOM');
DELETE FROM control_plane.schedule_revisions WHERE preset = 'CUSTOM';
DELETE FROM control_plane.schedules WHERE preset = 'CUSTOM';

ALTER TABLE control_plane.schedules
    DROP CONSTRAINT schedules_preset_check,
    DROP CONSTRAINT schedules_cron_expression_check,
    ADD CONSTRAINT schedules_preset_check
        CHECK (preset IN ('HOURLY', 'DAILY', 'WEEKDAYS', 'WEEKLY')),
    ADD CONSTRAINT schedules_cron_expression_check
        CHECK (char_length(cron_expression) BETWEEN 9 AND 32);

ALTER TABLE control_plane.schedule_revisions
    DROP CONSTRAINT schedule_revisions_preset_check,
    DROP CONSTRAINT schedule_revisions_cron_expression_check,
    ADD CONSTRAINT schedule_revisions_preset_check
        CHECK (preset IN ('HOURLY', 'DAILY', 'WEEKDAYS', 'WEEKLY')),
    ADD CONSTRAINT schedule_revisions_cron_expression_check
        CHECK (char_length(cron_expression) BETWEEN 9 AND 32);

RESET ROLE;
