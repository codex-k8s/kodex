-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.managed_configuration_sets
    DROP CONSTRAINT managed_configuration_sets_kind_check,
    DROP CONSTRAINT managed_configuration_sets_exact_scope,
    ADD CONSTRAINT managed_configuration_sets_kind_check CHECK (kind IN
        ('PROMPT_TEMPLATE','ROLE_IMAGE','INTEGRATION_DEFINITION','SYSTEM_STT','EMAIL_MAILBOX')),
    ADD CONSTRAINT managed_configuration_sets_exact_scope CHECK (
        (kind IN ('SYSTEM_STT','INTEGRATION_DEFINITION','EMAIL_MAILBOX') AND project_id IS NULL) OR
        (kind IN ('PROMPT_TEMPLATE','ROLE_IMAGE') AND project_id IS NOT NULL));

CREATE TABLE control_plane.email_mailbox_configuration_sets (
    configuration_set_id uuid PRIMARY KEY REFERENCES control_plane.managed_configuration_sets(id),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
    mailbox_ref text NOT NULL UNIQUE CHECK (mailbox_ref ~ '^mailbox-[0-9a-f]{32}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id,connection_id,configuration_set_id)
);
CREATE TRIGGER email_mailbox_configuration_owner_immutable
    BEFORE UPDATE OR DELETE ON control_plane.email_mailbox_configuration_sets
    FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();

CREATE TABLE control_plane.email_mailbox_configuration_bindings (
    connection_id uuid PRIMARY KEY REFERENCES control_plane.integration_connections(id),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    configuration_set_id uuid NOT NULL,
    revision_id uuid NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version>0),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (organization_id,connection_id,configuration_set_id)
        REFERENCES control_plane.email_mailbox_configuration_sets(organization_id,connection_id,configuration_set_id),
    FOREIGN KEY (configuration_set_id,revision_id)
        REFERENCES control_plane.managed_configuration_revisions(configuration_set_id,id)
);
GRANT SELECT,INSERT ON control_plane.email_mailbox_configuration_sets TO control_plane_runtime;
GRANT SELECT,INSERT,UPDATE,DELETE ON control_plane.email_mailbox_configuration_bindings TO control_plane_runtime;
RESET ROLE;
