-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.email_configuration_watermark (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL CHECK (revision > 0),
    digest text NOT NULL CHECK (digest ~ '^[0-9a-f]{64}$')
);
CREATE TABLE control_plane.email_mailbox_projections (
    ref text PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
    revision bigint NOT NULL CHECK (revision > 0),
    credential_generation bigint NOT NULL CHECK (credential_generation > 0),
    document_revision bigint NOT NULL CHECK (document_revision > 0),
    source_digest text NOT NULL CHECK (source_digest ~ '^[0-9a-f]{64}$'),
    enabled boolean NOT NULL,
    removed boolean NOT NULL DEFAULT false,
    safe_projection jsonb NOT NULL CHECK (jsonb_typeof(safe_projection)='object')
);
CREATE TABLE control_plane.email_mailbox_revisions (
    mailbox_ref text NOT NULL REFERENCES control_plane.email_mailbox_projections(ref),
    revision bigint NOT NULL CHECK (revision > 0),
    source_digest text NOT NULL CHECK (source_digest ~ '^[0-9a-f]{64}$'),
    safe_projection jsonb NOT NULL CHECK (jsonb_typeof(safe_projection)='object'),
    PRIMARY KEY (mailbox_ref, revision)
);
CREATE TRIGGER email_mailbox_revision_immutable BEFORE UPDATE OR DELETE ON control_plane.email_mailbox_revisions
    FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();

-- +goose StatementBegin
CREATE FUNCTION control_plane.accept_email_configuration(next_revision bigint, next_digest text, mailboxes jsonb)
RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE accepted bigint; mailbox jsonb; organization uuid; connection uuid;
    previous control_plane.email_mailbox_projections%ROWTYPE;
BEGIN
    IF jsonb_typeof(mailboxes) <> 'array' OR jsonb_array_length(mailboxes) > 100 THEN
        RAISE EXCEPTION 'email configuration is invalid';
    END IF;
    INSERT INTO control_plane.email_configuration_watermark AS current(singleton,revision,digest)
    VALUES(true,next_revision,next_digest)
    ON CONFLICT(singleton) DO UPDATE SET revision=EXCLUDED.revision,digest=EXCLUDED.digest
    WHERE current.revision < EXCLUDED.revision OR (current.revision=EXCLUDED.revision AND current.digest=EXCLUDED.digest)
    RETURNING revision INTO accepted;
    IF accepted IS NULL THEN RETURN false; END IF;
    FOR mailbox IN SELECT value FROM jsonb_array_elements(mailboxes) LOOP
        SELECT o.id,c.id INTO organization,connection
        FROM control_plane.organizations o
        JOIN control_plane.integration_connections c ON c.organization_id=o.id
        WHERE o.ref=mailbox->>'organization_ref' AND c.ref=mailbox->>'connection_ref'
          AND c.definition_key='email'
          AND c.public_configuration->>'mailbox_id'=mailbox->>'ref'
          AND c.public_configuration->>'from_address'=mailbox->>'sender';
        IF NOT FOUND THEN RAISE EXCEPTION 'email mailbox owner does not match connection'; END IF;
        SELECT * INTO previous FROM control_plane.email_mailbox_projections WHERE ref=mailbox->>'ref' FOR UPDATE;
        IF FOUND AND (previous.organization_id <> organization OR previous.connection_id <> connection
            OR previous.revision > (mailbox->>'revision')::bigint
            OR previous.credential_generation > (mailbox->>'credential_generation')::bigint
            OR (previous.revision=(mailbox->>'revision')::bigint
                AND (previous.source_digest <> mailbox->>'source_digest'
                     OR previous.safe_projection <> mailbox OR previous.removed))) THEN
            RAISE EXCEPTION 'email mailbox configuration rollback or identity mismatch';
        END IF;
        INSERT INTO control_plane.email_mailbox_projections(ref,organization_id,connection_id,revision,
            credential_generation,document_revision,source_digest,enabled,safe_projection)
        VALUES(mailbox->>'ref',organization,connection,(mailbox->>'revision')::bigint,
            (mailbox->>'credential_generation')::bigint,next_revision,mailbox->>'source_digest',
            (mailbox->>'enabled')::boolean,mailbox)
        ON CONFLICT(ref) DO UPDATE SET revision=EXCLUDED.revision,credential_generation=EXCLUDED.credential_generation,
            document_revision=EXCLUDED.document_revision,source_digest=EXCLUDED.source_digest,
            enabled=EXCLUDED.enabled,removed=false,safe_projection=EXCLUDED.safe_projection;
        INSERT INTO control_plane.email_mailbox_revisions(mailbox_ref,revision,source_digest,safe_projection)
        VALUES(mailbox->>'ref',(mailbox->>'revision')::bigint,mailbox->>'source_digest',mailbox)
        ON CONFLICT(mailbox_ref,revision) DO NOTHING;
    END LOOP;
    UPDATE control_plane.email_mailbox_projections SET enabled=false,removed=true,document_revision=next_revision
    WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(mailboxes) value WHERE value->>'ref'=ref);
    RETURN true;
END;
$$;
-- +goose StatementEnd

GRANT SELECT,INSERT,UPDATE ON control_plane.email_configuration_watermark,control_plane.email_mailbox_projections TO control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.email_mailbox_revisions TO control_plane_runtime;
REVOKE ALL ON FUNCTION control_plane.accept_email_configuration(bigint,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.accept_email_configuration(bigint,text,jsonb) TO control_plane_runtime;
RESET ROLE;
