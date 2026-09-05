-- +goose Up
SET ROLE control_plane_owner;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.validate_email_reconciliation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE receipt control_plane.email_effect_receipts%ROWTYPE;
BEGIN
    SELECT * INTO receipt FROM control_plane.email_effect_receipts WHERE id=NEW.receipt_id FOR UPDATE;
    IF NOT FOUND OR receipt.organization_id <> NEW.organization_id OR receipt.version <> NEW.receipt_version
       OR receipt.external_receipt_digest <> NEW.receipt_digest OR receipt.outcome <> 'UNKNOWN_OUTCOME'
       OR NOT EXISTS(SELECT 1 FROM control_plane.subjects actor
                     WHERE actor.id=NEW.actor_id AND actor.organization_id=NEW.organization_id)
       OR NOT EXISTS(SELECT 1 FROM control_plane.integration_invocations invocation
                     WHERE invocation.id=receipt.invocation_id AND invocation.state IN ('UNKNOWN_OUTCOME','CANCELLED','FAILED'))
       OR EXISTS(SELECT 1 FROM control_plane.email_reconciliation_decisions previous
                 WHERE previous.receipt_id=NEW.receipt_id AND previous.outcome <> NEW.outcome) THEN
        RAISE EXCEPTION 'email reconciliation does not match unresolved receipt';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
RESET ROLE;
