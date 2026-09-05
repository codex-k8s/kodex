-- +goose Up
SET ROLE control_plane_owner;
CREATE TABLE control_plane.interaction_identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE,
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
    connection_version bigint NOT NULL CHECK (connection_version > 0),
    external_team_ref text NOT NULL CHECK (length(external_team_ref) BETWEEN 1 AND 128),
    external_channel_ref text NOT NULL CHECK (length(external_channel_ref) BETWEEN 1 AND 128),
    external_user_digest text NOT NULL CHECK (external_user_digest ~ '^[a-f0-9]{64}$'),
    subject_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    state text NOT NULL DEFAULT 'ACTIVE' CHECK (state IN ('ACTIVE','REVOKED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    revoked_by uuid REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    revoked_at timestamptz
);
CREATE UNIQUE INDEX interaction_identities_active_identity
ON control_plane.interaction_identities (organization_id, connection_id, external_team_ref, external_channel_ref, external_user_digest)
WHERE state='ACTIVE';
GRANT SELECT, INSERT, UPDATE ON control_plane.interaction_identities TO control_plane_runtime;
ALTER TABLE control_plane.interaction_message_receipts
    ADD COLUMN identity_id uuid REFERENCES control_plane.interaction_identities(id),
    ADD COLUMN subject_id uuid REFERENCES control_plane.subjects(id);
RESET ROLE;
