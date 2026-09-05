-- +goose Up
SET ROLE control_plane_owner;
CREATE TABLE control_plane.email_credential_descriptors (
    name text NOT NULL CHECK (name ~ '^email-[0-9a-f]{32}$'),
    generation bigint NOT NULL CHECK (generation>0),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
    kind text NOT NULL CHECK (kind IN ('CA_CERTIFICATE','USERNAME','AUTH_SECRET')),
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    secret_ref text NOT NULL,
    secret_uid text NOT NULL CHECK (secret_uid<>''),
    secret_resource_version text NOT NULL CHECK (secret_resource_version<>''),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(name,generation)
);
CREATE TRIGGER email_credential_descriptor_immutable BEFORE UPDATE OR DELETE ON control_plane.email_credential_descriptors
    FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
GRANT SELECT, INSERT ON control_plane.email_credential_descriptors TO control_plane_runtime;
RESET ROLE;
