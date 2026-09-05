-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.integration_invocations
    DROP CONSTRAINT integration_invocations_approval_check,
    ADD CONSTRAINT integration_invocations_approval_check CHECK (
        (risk='READ' AND approval_policy='NONE' AND (state<>'WAITING_APPROVAL' OR mailbox_gate_required)) OR
        (risk IN ('WRITE','SENSITIVE','DESTRUCTIVE') AND approval_policy='HUMAN_EACH_EFFECT') OR
        (risk IN ('WRITE','SENSITIVE','DESTRUCTIVE') AND approval_policy='NONE' AND resource_kind='EMAIL_SENDER'
            AND (state<>'WAITING_APPROVAL' OR mailbox_gate_required))
    );

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_email_mailbox_gate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' AND (NEW.mailbox_gate_required IS DISTINCT FROM OLD.mailbox_gate_required
        OR NEW.approval_policy IS DISTINCT FROM OLD.approval_policy) THEN
        RAISE EXCEPTION 'mailbox approval policy is immutable';
    END IF;
    IF (NEW.mailbox_gate_required OR (NEW.risk<>'READ' AND NEW.approval_policy='NONE')) AND NOT EXISTS (
        SELECT 1 FROM control_plane.integration_connections c
        WHERE c.id=NEW.connection_id AND c.organization_id=NEW.organization_id AND c.definition_key='email'
    ) THEN RAISE EXCEPTION 'mailbox approval requires email owner connection'; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
RESET ROLE;
