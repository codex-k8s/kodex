-- +goose Up
SET ROLE control_plane_owner;
CREATE TABLE control_plane.email_mailbox_git_sources (
    source text NOT NULL CHECK (length(source) BETWEEN 1 AND 512),
    mailbox_key text NOT NULL CHECK (length(mailbox_key) BETWEEN 1 AND 63),
    configuration_set_id uuid NOT NULL UNIQUE REFERENCES control_plane.email_mailbox_configuration_sets(configuration_set_id),
    PRIMARY KEY(source,mailbox_key)
);
CREATE TABLE control_plane.email_mailbox_git_imports (
    source text NOT NULL,
    source_revision bigint NOT NULL CHECK (source_revision>0),
    source_digest text NOT NULL CHECK (source_digest ~ '^[0-9a-f]{64}$'),
    publication_ref text REFERENCES control_plane.email_mailbox_publications(ref),
    PRIMARY KEY(source,source_revision)
);
CREATE TABLE control_plane.email_mailbox_publication_bindings (
    publication_ref text NOT NULL REFERENCES control_plane.email_mailbox_publications(ref),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
    connection_version bigint NOT NULL CHECK (connection_version>0),
    configuration_set_id uuid,
    revision_id uuid,
    PRIMARY KEY(publication_ref,connection_id),
    FOREIGN KEY(organization_id,connection_id,configuration_set_id)
        REFERENCES control_plane.email_mailbox_configuration_sets(organization_id,connection_id,configuration_set_id),
    FOREIGN KEY(configuration_set_id,revision_id)
        REFERENCES control_plane.managed_configuration_revisions(configuration_set_id,id),
    CHECK ((configuration_set_id IS NULL)=(revision_id IS NULL))
);
CREATE TRIGGER email_mailbox_git_source_immutable BEFORE UPDATE OR DELETE ON control_plane.email_mailbox_git_sources
    FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
CREATE TRIGGER email_mailbox_git_import_immutable BEFORE UPDATE OR DELETE ON control_plane.email_mailbox_git_imports
    FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
CREATE TRIGGER email_mailbox_publication_binding_immutable BEFORE UPDATE OR DELETE ON control_plane.email_mailbox_publication_bindings
    FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
GRANT SELECT,INSERT ON control_plane.email_mailbox_git_sources,control_plane.email_mailbox_git_imports,
    control_plane.email_mailbox_publication_bindings TO control_plane_runtime;
RESET ROLE;
