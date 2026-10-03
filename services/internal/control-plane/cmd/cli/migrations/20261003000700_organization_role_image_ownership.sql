-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.role_image_recipes ALTER COLUMN scope_kind DROP DEFAULT;
ALTER TABLE control_plane.managed_configuration_sets
    DROP CONSTRAINT managed_configuration_sets_exact_scope,
    ADD CONSTRAINT managed_configuration_sets_exact_scope CHECK (
        (kind IN ('SYSTEM_STT','INTEGRATION_DEFINITION','EMAIL_MAILBOX') AND project_id IS NULL) OR
        (kind='PROMPT_TEMPLATE' AND project_id IS NOT NULL) OR kind='ROLE_IMAGE');
ALTER TABLE control_plane.image_builds ADD COLUMN scope_kind text;
ALTER TABLE control_plane.image_artifacts ADD COLUMN scope_kind text;
ALTER TABLE control_plane.role_image_recipe_revisions ADD COLUMN scope_kind text;
ALTER TABLE control_plane.role_image_promotion_requests ADD COLUMN scope_kind text;

UPDATE control_plane.image_builds child SET scope_kind=recipe.scope_kind
FROM control_plane.role_image_recipes recipe WHERE recipe.id=child.recipe_id;
UPDATE control_plane.image_artifacts child SET scope_kind=recipe.scope_kind
FROM control_plane.role_image_recipes recipe WHERE recipe.id=child.recipe_id;
-- Историческая immutable запись получает только назначенный владельцем scope.
-- Отключение единственного immutable guard ограничено этой owner-транзакцией.
ALTER TABLE control_plane.role_image_recipe_revisions DISABLE TRIGGER protect_role_image_recipe_revision;
UPDATE control_plane.role_image_recipe_revisions child SET scope_kind=recipe.scope_kind
FROM control_plane.role_image_recipes recipe WHERE recipe.id=child.recipe_id;
ALTER TABLE control_plane.role_image_recipe_revisions ENABLE TRIGGER protect_role_image_recipe_revision;
UPDATE control_plane.role_image_promotion_requests child SET scope_kind=recipe.scope_kind
FROM control_plane.role_image_recipes recipe WHERE recipe.id=child.recipe_id;

ALTER TABLE control_plane.image_builds ALTER COLUMN scope_kind SET NOT NULL,
    ADD CONSTRAINT image_builds_owner_scope CHECK ((scope_kind='PROJECT' AND project_id IS NOT NULL) OR (scope_kind='ORGANIZATION' AND project_id IS NULL));
ALTER TABLE control_plane.image_artifacts ALTER COLUMN scope_kind SET NOT NULL,
    ADD CONSTRAINT image_artifacts_owner_scope CHECK ((scope_kind='PROJECT' AND project_id IS NOT NULL) OR (scope_kind='ORGANIZATION' AND project_id IS NULL));
ALTER TABLE control_plane.role_image_recipe_revisions ALTER COLUMN scope_kind SET NOT NULL,
    ADD CONSTRAINT role_image_recipe_revisions_owner_scope CHECK ((scope_kind='PROJECT' AND project_id IS NOT NULL) OR (scope_kind='ORGANIZATION' AND project_id IS NULL));
ALTER TABLE control_plane.role_image_promotion_requests ALTER COLUMN scope_kind SET NOT NULL,
    ADD CONSTRAINT role_image_promotion_requests_owner_scope CHECK ((scope_kind='PROJECT' AND project_id IS NOT NULL) OR (scope_kind='ORGANIZATION' AND project_id IS NULL));

-- Один авторитетный предикат используется list/count и защищённым event rejoin.
-- +goose StatementBegin
CREATE FUNCTION control_plane.organization_role_image_actor_allowed(tenant uuid, actor uuid)
RETURNS boolean LANGUAGE sql STABLE SET search_path=pg_catalog,control_plane AS $$
SELECT EXISTS (SELECT 1 FROM control_plane.memberships membership
    JOIN control_plane.subjects subject ON subject.id=membership.subject_id
      AND subject.organization_id=membership.organization_id AND subject.active
    WHERE membership.organization_id=tenant AND membership.subject_id=actor
      AND membership.project_id IS NULL AND membership.active
      AND membership.role IN ('OWNER','ADMINISTRATOR'))
  AND control_plane.catalog_resource_visible(tenant,actor,'organization.manage','ORGANIZATION',
      tenant,NULL,NULL,'{}'::jsonb,transaction_timestamp());
$$;
-- +goose StatementEnd

-- Scope и родительский владелец не меняются ни при retry, ни при promotion.
-- Существующие FK остаются единственными рёбрами purge-графа; проверки ниже
-- закрепляют nullable owner tuple без фиктивного project или обхода admission.
-- +goose StatementBegin
CREATE FUNCTION control_plane.enforce_role_image_recipe_owner() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
BEGIN
    IF TG_OP='UPDATE' AND ROW(NEW.organization_id,NEW.project_id,NEW.role_definition_id,NEW.scope_kind)
        IS DISTINCT FROM ROW(OLD.organization_id,OLD.project_id,OLD.role_definition_id,OLD.scope_kind) THEN
        RAISE EXCEPTION 'role image owner is immutable' USING ERRCODE='23514';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM control_plane.role_definitions role
        WHERE role.id=NEW.role_definition_id AND role.organization_id=NEW.organization_id
          AND role.project_id IS NOT DISTINCT FROM NEW.project_id)
       OR (NEW.scope_kind='ORGANIZATION' AND NOT EXISTS (
        SELECT 1 FROM control_plane.agents agent WHERE agent.organization_id=NEW.organization_id
          AND agent.project_id IS NULL AND agent.system_key='system-assistant'
          AND agent.role_definition_id=NEW.role_definition_id)) THEN
        RAISE EXCEPTION 'role image owner lineage mismatch' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER role_image_recipe_owner BEFORE INSERT OR UPDATE ON control_plane.role_image_recipes
FOR EACH ROW EXECUTE FUNCTION control_plane.enforce_role_image_recipe_owner();

-- +goose StatementBegin
CREATE FUNCTION control_plane.enforce_role_image_child_owner() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
DECLARE parent control_plane.role_image_recipes%ROWTYPE;
BEGIN
    SELECT * INTO parent FROM control_plane.role_image_recipes WHERE id=NEW.recipe_id FOR KEY SHARE;
    IF NOT FOUND OR NEW.organization_id<>parent.organization_id
       OR NEW.project_id IS DISTINCT FROM parent.project_id THEN
        RAISE EXCEPTION 'image child owner lineage mismatch' USING ERRCODE='23514';
    END IF;
    IF TG_OP='INSERT' AND NEW.scope_kind IS NULL THEN NEW.scope_kind:=parent.scope_kind; END IF;
    IF NEW.scope_kind IS DISTINCT FROM parent.scope_kind THEN
        RAISE EXCEPTION 'image child scope mismatch' USING ERRCODE='23514';
    END IF;
    IF TG_OP='UPDATE' AND ROW(NEW.organization_id,NEW.project_id,NEW.recipe_id,NEW.scope_kind)
        IS DISTINCT FROM ROW(OLD.organization_id,OLD.project_id,OLD.recipe_id,OLD.scope_kind) THEN
        RAISE EXCEPTION 'image child owner is immutable' USING ERRCODE='23514';
    END IF;
    IF TG_TABLE_NAME='image_artifacts' THEN
        IF TG_OP='UPDATE' AND NEW.build_id IS DISTINCT FROM OLD.build_id THEN
            RAISE EXCEPTION 'artifact build lineage is immutable' USING ERRCODE='23514';
        END IF;
        IF NOT EXISTS (SELECT 1 FROM control_plane.image_builds build
            WHERE build.id=NEW.build_id AND build.recipe_id=NEW.recipe_id
              AND build.organization_id=NEW.organization_id AND build.scope_kind=NEW.scope_kind
              AND build.project_id IS NOT DISTINCT FROM NEW.project_id) THEN
            RAISE EXCEPTION 'artifact build lineage mismatch' USING ERRCODE='23514';
        END IF;
    ELSIF TG_TABLE_NAME IN ('role_image_recipe_revisions','role_image_promotion_requests') THEN
        IF TG_OP='UPDATE' AND NEW.image_artifact_id IS DISTINCT FROM OLD.image_artifact_id THEN
            RAISE EXCEPTION 'promotion artifact lineage is immutable' USING ERRCODE='23514';
        END IF;
        IF NEW.image_artifact_id IS NOT NULL AND NOT EXISTS (
            SELECT 1 FROM control_plane.image_artifacts artifact
            WHERE artifact.id=NEW.image_artifact_id AND artifact.recipe_id=NEW.recipe_id
              AND artifact.organization_id=NEW.organization_id AND artifact.scope_kind=NEW.scope_kind
              AND artifact.project_id IS NOT DISTINCT FROM NEW.project_id) THEN
            RAISE EXCEPTION 'promotion artifact lineage mismatch' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER role_image_build_owner BEFORE INSERT OR UPDATE ON control_plane.image_builds
FOR EACH ROW EXECUTE FUNCTION control_plane.enforce_role_image_child_owner();
CREATE TRIGGER role_image_artifact_owner BEFORE INSERT OR UPDATE ON control_plane.image_artifacts
FOR EACH ROW EXECUTE FUNCTION control_plane.enforce_role_image_child_owner();
CREATE TRIGGER role_image_revision_owner BEFORE INSERT OR UPDATE ON control_plane.role_image_recipe_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.enforce_role_image_child_owner();
CREATE TRIGGER role_image_promotion_owner BEFORE INSERT OR UPDATE ON control_plane.role_image_promotion_requests
FOR EACH ROW EXECUTE FUNCTION control_plane.enforce_role_image_child_owner();

GRANT EXECUTE ON FUNCTION control_plane.organization_role_image_actor_allowed(uuid,uuid) TO control_plane_runtime;
RESET ROLE;

-- +goose Down
SELECT 1/0;
