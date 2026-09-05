-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.email_effect_receipts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE,
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    invocation_id uuid NOT NULL UNIQUE REFERENCES control_plane.integration_invocations(id),
    external_receipt_ref text NOT NULL CHECK (external_receipt_ref ~ '^[0-9a-f]{32}$'),
    external_receipt_digest text NOT NULL CHECK (external_receipt_digest ~ '^[0-9a-f]{64}$'),
    semantic_input_digest text NOT NULL CHECK (semantic_input_digest ~ '^[0-9a-f]{64}$'),
    effect_key text NOT NULL CHECK (octet_length(effect_key) BETWEEN 1 AND 128),
    mailbox_ref text NOT NULL CHECK (octet_length(mailbox_ref) BETWEEN 1 AND 128),
    configuration_revision bigint NOT NULL CHECK (configuration_revision > 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    outcome text NOT NULL DEFAULT 'UNKNOWN_OUTCOME'
        CHECK (outcome IN ('UNKNOWN_OUTCOME','EFFECT_CONFIRMED','NO_EFFECT_CONFIRMED')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (organization_id, mailbox_ref, external_receipt_ref),
    UNIQUE (id, organization_id)
);

CREATE TABLE control_plane.email_effect_observations (
    receipt_id uuid NOT NULL REFERENCES control_plane.email_effect_receipts(id),
    version bigint NOT NULL CHECK (version > 0),
    outcome text NOT NULL CHECK (outcome IN ('UNKNOWN_OUTCOME','EFFECT_CONFIRMED','NO_EFFECT_CONFIRMED')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (receipt_id, version)
);

CREATE TABLE control_plane.email_reconciliation_decisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE,
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    receipt_id uuid NOT NULL,
    receipt_version bigint NOT NULL CHECK (receipt_version > 0),
    receipt_digest text NOT NULL CHECK (receipt_digest ~ '^[0-9a-f]{64}$'),
    outcome text NOT NULL CHECK (outcome IN ('EFFECT_CONFIRMED','NO_EFFECT_CONFIRMED')),
    grant_ref text NOT NULL UNIQUE,
    actor_id uuid NOT NULL REFERENCES control_plane.subjects(id),
    note text NOT NULL CHECK (char_length(note) <= 2000),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL,
    FOREIGN KEY (receipt_id, organization_id) REFERENCES control_plane.email_effect_receipts(id, organization_id),
    FOREIGN KEY (receipt_id, receipt_version) REFERENCES control_plane.email_effect_observations(receipt_id, version),
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '2 minutes')
);
CREATE INDEX email_reconciliation_receipt ON control_plane.email_reconciliation_decisions(receipt_id,created_at DESC,ref DESC);

-- +goose StatementBegin
CREATE FUNCTION control_plane.validate_email_reconciliation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE receipt control_plane.email_effect_receipts%ROWTYPE;
BEGIN
    SELECT * INTO receipt FROM control_plane.email_effect_receipts WHERE id=NEW.receipt_id FOR UPDATE;
    IF NOT FOUND OR receipt.organization_id <> NEW.organization_id OR receipt.version <> NEW.receipt_version
       OR receipt.external_receipt_digest <> NEW.receipt_digest OR receipt.outcome <> 'UNKNOWN_OUTCOME'
       OR NOT EXISTS(SELECT 1 FROM control_plane.subjects actor
                     WHERE actor.id=NEW.actor_id AND actor.organization_id=NEW.organization_id)
       OR NOT EXISTS(SELECT 1 FROM control_plane.integration_invocations invocation
                     WHERE invocation.id=receipt.invocation_id AND invocation.state='UNKNOWN_OUTCOME')
       OR EXISTS(SELECT 1 FROM control_plane.email_reconciliation_decisions previous
                 WHERE previous.receipt_id=NEW.receipt_id AND previous.outcome <> NEW.outcome) THEN
        RAISE EXCEPTION 'email reconciliation does not match unresolved receipt';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER email_reconciliation_valid BEFORE INSERT ON control_plane.email_reconciliation_decisions
    FOR EACH ROW EXECUTE FUNCTION control_plane.validate_email_reconciliation();

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_email_effect_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.outcome <> 'UNKNOWN_OUTCOME' OR NEW.version <> 1 THEN
            RAISE EXCEPTION 'email receipt must begin with unknown outcome';
        END IF;
        IF NOT EXISTS(SELECT 1 FROM control_plane.integration_invocations invocation
                      JOIN control_plane.integration_connections connection ON connection.id=invocation.connection_id
                      WHERE invocation.id=NEW.invocation_id AND invocation.organization_id=NEW.organization_id
                        AND connection.organization_id=NEW.organization_id AND connection.definition_key='email'
                        AND invocation.effect_key=NEW.effect_key) THEN
            RAISE EXCEPTION 'email receipt does not match owner invocation';
        END IF;
        RETURN NEW;
    END IF;
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'email receipt cannot be deleted'; END IF;
    IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.invocation_id,NEW.external_receipt_ref,
           NEW.external_receipt_digest,NEW.semantic_input_digest,NEW.effect_key,NEW.mailbox_ref,
           NEW.configuration_revision,NEW.created_at)
       IS DISTINCT FROM
       ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.invocation_id,OLD.external_receipt_ref,
           OLD.external_receipt_digest,OLD.semantic_input_digest,OLD.effect_key,OLD.mailbox_ref,
           OLD.configuration_revision,OLD.created_at)
       OR OLD.outcome <> 'UNKNOWN_OUTCOME'
       OR NEW.outcome NOT IN ('EFFECT_CONFIRMED','NO_EFFECT_CONFIRMED')
       OR NEW.version <> OLD.version + 1 OR NEW.updated_at < OLD.updated_at THEN
        RAISE EXCEPTION 'email receipt identity or outcome transition is invalid';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER email_receipt_transition BEFORE INSERT OR UPDATE OR DELETE ON control_plane.email_effect_receipts
    FOR EACH ROW EXECUTE FUNCTION control_plane.protect_email_effect_receipt();

-- +goose StatementBegin
CREATE FUNCTION control_plane.observe_email_effect_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO control_plane.email_effect_observations(receipt_id,version,outcome,created_at)
    VALUES(NEW.id,NEW.version,NEW.outcome,NEW.updated_at);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER email_receipt_observation AFTER INSERT OR UPDATE ON control_plane.email_effect_receipts
    FOR EACH ROW EXECUTE FUNCTION control_plane.observe_email_effect_receipt();
CREATE TRIGGER email_observation_immutable BEFORE UPDATE OR DELETE ON control_plane.email_effect_observations
    FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
CREATE TRIGGER email_reconciliation_immutable BEFORE UPDATE OR DELETE ON control_plane.email_reconciliation_decisions
    FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();

GRANT SELECT,INSERT,UPDATE ON control_plane.email_effect_receipts TO control_plane_runtime;
GRANT SELECT,INSERT ON control_plane.email_effect_observations,control_plane.email_reconciliation_decisions TO control_plane_runtime;
RESET ROLE;
