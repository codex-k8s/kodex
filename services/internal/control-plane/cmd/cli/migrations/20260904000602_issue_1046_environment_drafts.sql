-- +goose Up
SET ROLE control_plane_owner;
CREATE TABLE control_plane.runtime_environment_drafts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE,
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid NOT NULL REFERENCES control_plane.projects(id),
    environment_ref text NOT NULL DEFAULT '',
    expected_environment_version bigint NOT NULL DEFAULT 0,
    state text NOT NULL CHECK (state IN ('DRAFT', 'VALID', 'INVALID', 'PUBLISHED', 'DISCARDED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    specification jsonb NOT NULL CHECK (jsonb_typeof(specification) = 'object' AND octet_length(specification::text) <= 262144),
    validation_digest text NOT NULL DEFAULT '',
    diagnostics jsonb NOT NULL DEFAULT '[]'::jsonb,
    published_environment_ref text NOT NULL DEFAULT '',
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((environment_ref = '' AND expected_environment_version = 0) OR (environment_ref <> '' AND expected_environment_version > 0))
);
GRANT SELECT, INSERT, UPDATE ON control_plane.runtime_environment_drafts TO control_plane_runtime;
RESET ROLE;
