-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.integration_invocations
    ADD COLUMN mailbox_gate_required boolean NOT NULL DEFAULT false,
    DROP CONSTRAINT integration_invocations_approval_check,
    ADD CONSTRAINT integration_invocations_approval_check CHECK (
        (risk='READ' AND approval_policy='NONE' AND (state<>'WAITING_APPROVAL' OR mailbox_gate_required)) OR
        (risk IN ('WRITE','SENSITIVE','DESTRUCTIVE') AND approval_policy='HUMAN_EACH_EFFECT')
    );

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_email_mailbox_gate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND NEW.mailbox_gate_required IS DISTINCT FROM OLD.mailbox_gate_required THEN
        RAISE EXCEPTION 'mailbox gate requirement is immutable';
    END IF;
    IF NEW.mailbox_gate_required AND NOT EXISTS (
        SELECT 1 FROM control_plane.integration_connections c
        WHERE c.id=NEW.connection_id AND c.organization_id=NEW.organization_id AND c.definition_key='email'
    ) THEN RAISE EXCEPTION 'mailbox gate requires email owner connection'; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER email_mailbox_gate_valid BEFORE INSERT OR UPDATE ON control_plane.integration_invocations
    FOR EACH ROW EXECUTE FUNCTION control_plane.protect_email_mailbox_gate();

CREATE TABLE control_plane.email_authorizations (
    ref text PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    invocation_id uuid REFERENCES control_plane.integration_invocations(id),
    connection_test_id uuid REFERENCES control_plane.integration_connection_tests(id),
    source_ref text NOT NULL,
    lease_ref text NOT NULL,
    fence_digest text NOT NULL CHECK (fence_digest ~ '^[0-9a-f]{64}$'),
    generation bigint NOT NULL CHECK (generation > 0),
    semantic_input_digest text NOT NULL CHECK (semantic_input_digest ~ '^[0-9a-f]{64}$'),
    query_projection jsonb NOT NULL CHECK (jsonb_typeof(query_projection)='object'),
    decision_projection jsonb NOT NULL CHECK (jsonb_typeof(decision_projection)='object'),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((invocation_id IS NULL) <> (connection_test_id IS NULL)),
    UNIQUE(organization_id,source_ref,lease_ref,generation)
);
CREATE TRIGGER email_authorization_immutable BEFORE UPDATE OR DELETE ON control_plane.email_authorizations
    FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();
ALTER TABLE control_plane.email_effect_receipts ADD COLUMN authorization_ref text REFERENCES control_plane.email_authorizations(ref);

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_email_receipt_authorization() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND NEW.authorization_ref IS DISTINCT FROM OLD.authorization_ref THEN
        RAISE EXCEPTION 'email receipt authorization is immutable';
    END IF;
    IF NEW.authorization_ref IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM control_plane.email_authorizations a
        WHERE a.ref=NEW.authorization_ref AND a.organization_id=NEW.organization_id
          AND a.invocation_id=NEW.invocation_id AND a.semantic_input_digest=NEW.semantic_input_digest
    ) THEN RAISE EXCEPTION 'email receipt authorization does not match invocation'; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER email_receipt_authorization_valid BEFORE INSERT OR UPDATE ON control_plane.email_effect_receipts
    FOR EACH ROW EXECUTE FUNCTION control_plane.protect_email_receipt_authorization();
GRANT SELECT,INSERT ON control_plane.email_authorizations TO control_plane_runtime;
RESET ROLE;
