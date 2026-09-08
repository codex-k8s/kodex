-- +goose Up
SET ROLE control_plane_owner;
-- Immutable publication и её original version остаются исходной provenance.
-- Только служебное продвижение exact OCC получает отдельный append-only receipt.
CREATE TABLE control_plane.email_mailbox_observation_receipts (
    publication_ref text NOT NULL REFERENCES control_plane.email_mailbox_publications(ref),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
    previous_version bigint NOT NULL CHECK (previous_version>0),
    current_version bigint NOT NULL CHECK (current_version=previous_version+1),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(publication_ref,connection_id,current_version),
    UNIQUE(publication_ref,connection_id,previous_version)
);
CREATE TRIGGER email_mailbox_observation_receipt_immutable BEFORE UPDATE OR DELETE
    ON control_plane.email_mailbox_observation_receipts
    FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
GRANT SELECT,INSERT ON control_plane.email_mailbox_observation_receipts TO control_plane_runtime;
RESET ROLE;
