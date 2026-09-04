-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.runtime_secret_revisions ADD COLUMN state text NOT NULL DEFAULT 'ACTIVE'
    CHECK (state IN ('ACTIVE','RETIRED'));
-- Старый recovery мог уже удалить эти объекты без устойчивого подтверждения.
UPDATE control_plane.runtime_secret_revisions revision SET state='RETIRED'
FROM control_plane.runtime_secrets secret WHERE revision.secret_id=secret.id AND revision.revision<>secret.current_revision;
GRANT UPDATE(state) ON control_plane.runtime_secret_revisions TO control_plane_runtime;
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_runtime_secret_revision_retirement()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.state = 'RETIRED' AND NEW.state <> 'RETIRED' THEN
        RAISE EXCEPTION 'runtime secret revision retirement is irreversible';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_runtime_secret_revision_retirement
BEFORE UPDATE OF state ON control_plane.runtime_secret_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_runtime_secret_revision_retirement();
RESET ROLE;
