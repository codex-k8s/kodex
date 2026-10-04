-- +goose Up
SET ROLE control_plane_owner;

-- SQL eligibility использует тот же закрытый реестр resource kinds,
-- что и текущий доменный evaluator. Обновление допускается только из
-- точного прежнего определения, без исправления неизвестного состояния.
-- +goose StatementBegin
DO $$
DECLARE entry record;
BEGIN
 FOR entry IN SELECT * FROM (VALUES
  ('secret.view',ARRAY['PROJECT','SECRET']::text[],ARRAY['ORGANIZATION','PROJECT','SECRET']::text[]),
  ('secret.create',ARRAY['PROJECT']::text[],ARRAY['ORGANIZATION','PROJECT']::text[]),
  ('secret.rotate',ARRAY['SECRET']::text[],ARRAY['ORGANIZATION','SECRET']::text[]),
  ('secret.revoke',ARRAY['SECRET']::text[],ARRAY['ORGANIZATION','SECRET']::text[]),
  ('secret.reveal',ARRAY['SECRET']::text[],ARRAY['ORGANIZATION','SECRET']::text[])
 ) AS definitions(permission_key,previous_kinds,current_kinds)
 LOOP
  UPDATE control_plane.permission_registry SET resource_kinds=entry.current_kinds
  WHERE permission_key=entry.permission_key AND resource_kinds=entry.previous_kinds;
  IF NOT FOUND THEN
   RAISE EXCEPTION 'secret permission registry definition does not match previous revision' USING ERRCODE='23514';
  END IF;
 END LOOP;
END;
$$;
-- +goose StatementEnd

-- Старые immutable планы остаются историей, но не читаются и не применяются
-- новым протоколом. Даже план без items обязан иметь явную версию владельца.
ALTER TABLE control_plane.revision_impact_plans ADD COLUMN owner_snapshot_revision bigint NOT NULL DEFAULT 1;
ALTER TABLE control_plane.role_image_impact_plans ADD COLUMN owner_snapshot_revision bigint NOT NULL DEFAULT 1;
ALTER TABLE control_plane.runtime_secret_draft_impact_plans ADD COLUMN owner_snapshot_revision bigint NOT NULL DEFAULT 1;
ALTER TABLE control_plane.revision_impact_plans ALTER COLUMN owner_snapshot_revision DROP DEFAULT;
ALTER TABLE control_plane.role_image_impact_plans ALTER COLUMN owner_snapshot_revision DROP DEFAULT;
ALTER TABLE control_plane.runtime_secret_draft_impact_plans ALTER COLUMN owner_snapshot_revision DROP DEFAULT;

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_impact_owner_snapshot_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' AND NEW.owner_snapshot_revision IS DISTINCT FROM 2 THEN
  RAISE EXCEPTION 'impact owner snapshot revision is invalid' USING ERRCODE='23514';
 END IF;
 IF TG_OP='UPDATE' AND NEW.owner_snapshot_revision IS DISTINCT FROM OLD.owner_snapshot_revision THEN
  RAISE EXCEPTION 'impact owner snapshot revision is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER revision_impact_owner_snapshot_guard BEFORE INSERT OR UPDATE ON control_plane.revision_impact_plans
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_impact_owner_snapshot_revision();
CREATE TRIGGER role_image_impact_owner_snapshot_guard BEFORE INSERT OR UPDATE ON control_plane.role_image_impact_plans
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_impact_owner_snapshot_revision();
CREATE TRIGGER secret_draft_impact_owner_snapshot_guard BEFORE INSERT OR UPDATE ON control_plane.runtime_secret_draft_impact_plans
 FOR EACH ROW EXECUTE FUNCTION control_plane.guard_impact_owner_snapshot_revision();
RESET ROLE;

-- +goose Down
SELECT 1/0;
