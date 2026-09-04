-- +goose Up
SET ROLE control_plane_owner;

-- IntegrationDefinition описывает organization-wide IntegrationConnection и
-- не наследует проектную область от UI locator.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM control_plane.managed_configuration_sets
        WHERE kind = 'INTEGRATION_DEFINITION' AND project_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'project-scoped integration definition must be detached or copied before remediation';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE control_plane.managed_configuration_sets
    ADD CONSTRAINT managed_configuration_sets_exact_scope CHECK (
        (kind IN ('SYSTEM_STT', 'INTEGRATION_DEFINITION') AND project_id IS NULL) OR
        (kind IN ('PROMPT_TEMPLATE', 'ROLE_IMAGE') AND project_id IS NOT NULL)
    );

UPDATE control_plane.permission_registry
SET resource_kinds = ARRAY['ORGANIZATION','PROJECT','AGENT','WORKFLOW','RUN','SESSION','SCHEDULE']
WHERE permission_key = 'prompt.full.view';

CREATE TABLE control_plane.agent_avatar_upload_reservations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^avres_[A-Za-z0-9_-]{8,88}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    agent_id uuid NOT NULL REFERENCES control_plane.agents(id),
    actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    operation text NOT NULL CHECK (operation = 'agent.avatar.upload'),
    idempotency_key text NOT NULL CHECK (char_length(idempotency_key) BETWEEN 8 AND 128),
    intent_digest text NOT NULL CHECK (intent_digest ~ '^[a-f0-9]{64}$'),
    expected_agent_version bigint NOT NULL CHECK (expected_agent_version > 0),
    artifact_ref text NOT NULL UNIQUE CHECK (artifact_ref ~ '^art_[A-Za-z0-9_-]{8,89}$'),
    file_name text NOT NULL CHECK (char_length(file_name) BETWEEN 1 AND 255),
    media_type text NOT NULL CHECK (media_type IN ('image/jpeg', 'image/png', 'image/webp')),
    size_bytes bigint NOT NULL CHECK (size_bytes BETWEEN 1 AND 5242880),
    digest text NOT NULL CHECK (digest ~ '^sha256:[a-f0-9]{64}$'),
    object_key text NOT NULL UNIQUE CHECK (char_length(object_key) BETWEEN 1 AND 1024),
    object_version text NOT NULL DEFAULT '' CHECK (char_length(object_version) <= 512),
    object_etag text NOT NULL DEFAULT '' CHECK (char_length(object_etag) <= 512),
    state text NOT NULL CHECK (state IN ('RESERVED', 'MATERIALIZED', 'FINALIZED', 'COMPENSATING', 'COMPENSATED')),
    expires_at timestamptz NOT NULL,
    cleanup_claimed_at timestamptz,
    finalized_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, actor_id, operation, idempotency_key),
    CHECK ((state = 'FINALIZED') = (finalized_at IS NOT NULL)),
    CHECK (state NOT IN ('MATERIALIZED', 'FINALIZED') OR object_etag <> ''),
    CHECK (state <> 'RESERVED' OR (object_version = '' AND object_etag = ''))
);

CREATE INDEX agent_avatar_upload_expiry
    ON control_plane.agent_avatar_upload_reservations (expires_at, ref)
    WHERE state IN ('RESERVED', 'MATERIALIZED', 'COMPENSATING');

GRANT SELECT, INSERT, UPDATE ON control_plane.agent_avatar_upload_reservations TO control_plane_runtime;

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

DROP TABLE control_plane.agent_avatar_upload_reservations;
UPDATE control_plane.permission_registry
SET resource_kinds = ARRAY['ORGANIZATION','AGENT','WORKFLOW','RUN','SESSION','SCHEDULE']
WHERE permission_key = 'prompt.full.view';
ALTER TABLE control_plane.managed_configuration_sets
    DROP CONSTRAINT managed_configuration_sets_exact_scope;

RESET ROLE;
