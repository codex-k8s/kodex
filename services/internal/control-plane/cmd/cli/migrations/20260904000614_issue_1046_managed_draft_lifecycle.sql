-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.managed_configuration_revisions DROP CONSTRAINT managed_configuration_revisions_state_check;
ALTER TABLE control_plane.managed_configuration_revisions ADD CONSTRAINT managed_configuration_revisions_state_check
    CHECK(state IN ('DRAFT','VALID','INVALID','PUBLISHED','SUPERSEDED','DISCARDED'));
ALTER TABLE control_plane.managed_configuration_revisions DROP CONSTRAINT managed_configuration_revisions_content_check;
ALTER TABLE control_plane.managed_configuration_revisions ADD CONSTRAINT managed_configuration_revisions_content_check
    CHECK(octet_length(content)<=262144);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_managed_configuration_revision()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'managed configuration revision is immutable'; END IF;
    IF ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.configuration_set_id,OLD.revision,
           OLD.content_format,OLD.content,OLD.digest,OLD.parent_revision_id,OLD.created_by,OLD.created_at)
       IS DISTINCT FROM
       ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.configuration_set_id,NEW.revision,
           NEW.content_format,NEW.content,NEW.digest,NEW.parent_revision_id,NEW.created_by,NEW.created_at)
       OR NOT ((OLD.state IN ('DRAFT','INVALID') AND NEW.state IN ('VALID','INVALID')) OR
               (OLD.state='VALID' AND NEW.state='PUBLISHED') OR
               (OLD.state='PUBLISHED' AND NEW.state='SUPERSEDED') OR
               (OLD.state IN ('DRAFT','INVALID','VALID') AND NEW.state='DISCARDED')) THEN
        RAISE EXCEPTION 'managed configuration revision is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
RESET ROLE;
