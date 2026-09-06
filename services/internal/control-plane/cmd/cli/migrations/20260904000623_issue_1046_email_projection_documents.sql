-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.email_configuration_documents (
    revision bigint PRIMARY KEY CHECK (revision > 0),
    digest text NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    document jsonb NOT NULL CHECK (jsonb_typeof(document) = 'object'
        AND document->>'version' = 'email-bridge/v1'
        AND (document->>'revision')::bigint = revision),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER email_configuration_document_immutable
    BEFORE UPDATE OR DELETE ON control_plane.email_configuration_documents
    FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
GRANT SELECT, INSERT ON control_plane.email_configuration_documents TO control_plane_runtime;
RESET ROLE;
