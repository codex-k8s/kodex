-- +goose Up
SET ROLE control_plane_owner;
CREATE TABLE control_plane.email_mailbox_publications (
    ref text PRIMARY KEY CHECK (ref ~ '^mailpub_[A-Za-z0-9_-]{8,89}$'),
    revision bigint NOT NULL UNIQUE CHECK (revision>0),
    digest text NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$'),
    document jsonb NOT NULL CHECK (jsonb_typeof(document)='object' AND octet_length(document::text)<=921600),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
    connection_version bigint NOT NULL CHECK (connection_version>0),
    configuration_set_id uuid,
    configuration_revision_id uuid,
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    kind text NOT NULL CHECK (kind IN ('BIND','UNBIND','GIT_SYNC','RECOVERY')),
    state text NOT NULL DEFAULT 'PENDING' CHECK (state IN ('PENDING','READY','FAILED','SUPERSEDED')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '30 minutes',
    ready_at timestamptz,
    failure_code text NOT NULL DEFAULT '' CHECK (failure_code IN ('','EMAIL_MAILBOX_DELIVERY_EXPIRED','EMAIL_MAILBOX_CONNECTION_CHANGED','EMAIL_MAILBOX_DELIVERY_REJECTED')),
    claimant text NOT NULL DEFAULT '',
    claim_generation bigint NOT NULL DEFAULT 0 CHECK (claim_generation>=0),
    lease_expires_at timestamptz,
    policy_document jsonb,
    policy_digest text,
    applied_at timestamptz,
    callback_at timestamptz,
    FOREIGN KEY (organization_id,connection_id,configuration_set_id)
        REFERENCES control_plane.email_mailbox_configuration_sets(organization_id,connection_id,configuration_set_id),
    FOREIGN KEY (configuration_set_id,configuration_revision_id)
        REFERENCES control_plane.managed_configuration_revisions(configuration_set_id,id),
    CHECK ((configuration_set_id IS NULL)=(configuration_revision_id IS NULL)),
    CHECK (kind<>'BIND' OR configuration_set_id IS NOT NULL),
    CHECK ((policy_document IS NULL)=(policy_digest IS NULL)),
    CHECK (policy_digest IS NULL OR policy_digest ~ '^[0-9a-f]{64}$'),
    CHECK (policy_document IS NULL OR (jsonb_typeof(policy_document)='object' AND octet_length(policy_document::text)<=65536)),
    CHECK ((claimant='')=(lease_expires_at IS NULL)),
    CHECK (state<>'READY' OR (ready_at IS NOT NULL AND applied_at IS NOT NULL AND callback_at IS NOT NULL)),
    CHECK ((state='FAILED')=(failure_code<>''))
);
CREATE UNIQUE INDEX email_mailbox_publications_single_pending ON control_plane.email_mailbox_publications((true)) WHERE state='PENDING';
CREATE INDEX email_mailbox_publications_connection ON control_plane.email_mailbox_publications(organization_id,connection_id,revision DESC);

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_email_mailbox_publication() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' OR
       (NEW.ref,NEW.revision,NEW.digest,NEW.document,NEW.organization_id,NEW.connection_id,NEW.connection_version,
        NEW.configuration_set_id,NEW.configuration_revision_id,NEW.created_by,NEW.kind,NEW.created_at,NEW.expires_at)
       IS DISTINCT FROM
       (OLD.ref,OLD.revision,OLD.digest,OLD.document,OLD.organization_id,OLD.connection_id,OLD.connection_version,
        OLD.configuration_set_id,OLD.configuration_revision_id,OLD.created_by,OLD.kind,OLD.created_at,OLD.expires_at) OR
       (OLD.policy_document IS NOT NULL AND (NEW.policy_document,NEW.policy_digest) IS DISTINCT FROM (OLD.policy_document,OLD.policy_digest)) OR
       (OLD.state IN ('FAILED','SUPERSEDED') AND NEW IS DISTINCT FROM OLD) OR
       (OLD.state='READY' AND NEW.state NOT IN ('READY','SUPERSEDED')) OR NEW.claim_generation<OLD.claim_generation THEN
        RAISE EXCEPTION 'email publication immutable state cannot change';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER email_mailbox_publication_immutable BEFORE UPDATE OR DELETE ON control_plane.email_mailbox_publications
    FOR EACH ROW EXECUTE FUNCTION control_plane.protect_email_mailbox_publication();
GRANT SELECT,INSERT,UPDATE ON control_plane.email_mailbox_publications TO control_plane_runtime;
RESET ROLE;
