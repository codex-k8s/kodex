-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.runtime_secrets ALTER COLUMN scope_kind DROP DEFAULT;

-- Владелец секрета назначается один раз; операции не создают собственную область.
-- +goose StatementBegin
CREATE FUNCTION control_plane.enforce_runtime_secret_owner() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
BEGIN
    IF TG_OP='UPDATE' AND ROW(NEW.organization_id,NEW.project_id,NEW.scope_kind)
        IS DISTINCT FROM ROW(OLD.organization_id,OLD.project_id,OLD.scope_kind) THEN
        RAISE EXCEPTION 'runtime secret owner is immutable' USING ERRCODE='23514';
    END IF;
    IF NEW.project_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM control_plane.projects project
        WHERE project.id=NEW.project_id AND project.organization_id=NEW.organization_id) THEN
        RAISE EXCEPTION 'runtime secret project owner mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER runtime_secret_owner BEFORE INSERT OR UPDATE ON control_plane.runtime_secrets
FOR EACH ROW EXECUTE FUNCTION control_plane.enforce_runtime_secret_owner();

-- +goose StatementBegin
CREATE FUNCTION control_plane.enforce_runtime_secret_operation_owner() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
DECLARE parent control_plane.runtime_secrets%ROWTYPE;
BEGIN
    SELECT * INTO parent FROM control_plane.runtime_secrets WHERE id=NEW.secret_id FOR KEY SHARE;
    IF NOT FOUND OR NEW.organization_id<>parent.organization_id
       OR NEW.project_id IS DISTINCT FROM parent.project_id THEN
        RAISE EXCEPTION 'runtime secret operation owner mismatch' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' AND ROW(NEW.organization_id,NEW.project_id,NEW.secret_id,NEW.actor_id)
        IS DISTINCT FROM ROW(OLD.organization_id,OLD.project_id,OLD.secret_id,OLD.actor_id) THEN
        RAISE EXCEPTION 'runtime secret operation owner is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER runtime_secret_operation_owner BEFORE INSERT OR UPDATE ON control_plane.runtime_secret_operations
FOR EACH ROW EXECUTE FUNCTION control_plane.enforce_runtime_secret_operation_owner();

-- +goose StatementBegin
CREATE FUNCTION control_plane.enforce_runtime_secret_draft_owner() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM control_plane.runtime_secrets secret
        WHERE secret.id=NEW.secret_id AND secret.organization_id=NEW.organization_id) THEN
        RAISE EXCEPTION 'runtime secret draft owner mismatch' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' AND ROW(NEW.organization_id,NEW.secret_id,NEW.owner_actor_id)
        IS DISTINCT FROM ROW(OLD.organization_id,OLD.secret_id,OLD.owner_actor_id) THEN
        RAISE EXCEPTION 'runtime secret draft owner is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER runtime_secret_draft_owner BEFORE INSERT OR UPDATE ON control_plane.runtime_secret_drafts
FOR EACH ROW EXECUTE FUNCTION control_plane.enforce_runtime_secret_draft_owner();

RESET ROLE;
-- Forward-only: опубликованные организационные секреты не перемещаются в проект.
-- +goose Down
SELECT 1;
