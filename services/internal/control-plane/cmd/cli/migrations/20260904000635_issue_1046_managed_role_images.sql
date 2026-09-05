-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.role_image_recipes ADD CONSTRAINT role_image_recipes_managed_tenant UNIQUE(id,organization_id);
CREATE TABLE control_plane.managed_role_image_recipes (
 configuration_set_id uuid PRIMARY KEY,
 organization_id uuid NOT NULL,
 recipe_id uuid NOT NULL UNIQUE,
 origin text NOT NULL CHECK(origin IN ('BASELINE','MANAGED')),
 FOREIGN KEY(configuration_set_id,organization_id) REFERENCES control_plane.managed_configuration_sets(id,organization_id),
 FOREIGN KEY(recipe_id,organization_id) REFERENCES control_plane.role_image_recipes(id,organization_id)
);
CREATE TABLE control_plane.managed_role_image_revisions (
 configuration_revision_id uuid PRIMARY KEY REFERENCES control_plane.managed_configuration_revisions(id),
 configuration_set_id uuid NOT NULL REFERENCES control_plane.managed_role_image_recipes(configuration_set_id),
 recipe_generation bigint NOT NULL CHECK(recipe_generation>0),
 recipe_version bigint NOT NULL CHECK(recipe_version>0),
 UNIQUE(configuration_set_id,recipe_generation)
);
CREATE TABLE control_plane.managed_role_image_builds (
 build_id uuid PRIMARY KEY REFERENCES control_plane.image_builds(id),
 configuration_revision_id uuid NOT NULL REFERENCES control_plane.managed_role_image_revisions(configuration_revision_id)
);

-- +goose StatementBegin
DO $$
DECLARE recipe record; configuration_id uuid; revision_id uuid; content text;
BEGIN
 FOR recipe IN SELECT image.*,role.ref AS role_ref FROM control_plane.role_image_recipes image
   JOIN control_plane.role_definitions role ON role.id=image.role_definition_id AND role.organization_id=image.organization_id
 LOOP
  configuration_id:=gen_random_uuid(); revision_id:=gen_random_uuid();
  content:=jsonb_build_object('name',recipe.name,'roleImage',jsonb_build_object('roleDefinitionRef',recipe.role_ref,
   'environment',jsonb_build_object('environmentKey',COALESCE(recipe.specification->>'EnvironmentKey',''),
    'packageKeys',COALESCE(recipe.specification->'PackageKeys','[]'::jsonb),'toolKeys',COALESCE(recipe.specification->'ToolKeys','[]'::jsonb),
    'installationBlock',COALESCE(recipe.specification->>'InstallationBlock',''),'dockerfile',COALESCE(recipe.specification->>'Dockerfile',''))))::text;
  INSERT INTO control_plane.managed_configuration_sets(id,ref,organization_id,project_id,kind,name,managed_by,source,created_by)
   VALUES(configuration_id,'mcfg_'||replace(configuration_id::text,'-',''),recipe.organization_id,recipe.project_id,'ROLE_IMAGE',recipe.name,'UI','control-center',recipe.created_by);
  INSERT INTO control_plane.managed_configuration_revisions(id,ref,organization_id,configuration_set_id,revision,state,content_format,content,digest,created_by,validated_at,published_at)
   VALUES(revision_id,'mrev_'||replace(revision_id::text,'-',''),recipe.organization_id,configuration_id,1,'PUBLISHED','JSON',content,
    encode(digest(convert_to(content,'UTF8'),'sha256'),'hex'),recipe.created_by,clock_timestamp(),clock_timestamp());
  UPDATE control_plane.managed_configuration_sets SET current_revision_id=revision_id WHERE id=configuration_id;
  INSERT INTO control_plane.managed_role_image_recipes VALUES(configuration_id,recipe.organization_id,recipe.id,'BASELINE');
  INSERT INTO control_plane.managed_role_image_revisions VALUES(revision_id,configuration_id,recipe.generation,recipe.version);
  INSERT INTO control_plane.managed_role_image_builds
   SELECT build.id,revision_id FROM control_plane.image_builds build WHERE build.recipe_id=recipe.id AND build.organization_id=recipe.organization_id
    AND build.recipe_generation=recipe.generation AND build.spec_sha256=recipe.spec_sha256;
 END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.guard_managed_role_image_mapping() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'managed role image provenance is immutable';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER managed_role_image_recipes_guard BEFORE UPDATE OR DELETE ON control_plane.managed_role_image_recipes FOR EACH ROW EXECUTE FUNCTION control_plane.guard_managed_role_image_mapping();
CREATE TRIGGER managed_role_image_revisions_guard BEFORE UPDATE OR DELETE ON control_plane.managed_role_image_revisions FOR EACH ROW EXECUTE FUNCTION control_plane.guard_managed_role_image_mapping();
CREATE TRIGGER managed_role_image_builds_guard BEFORE UPDATE OR DELETE ON control_plane.managed_role_image_builds FOR EACH ROW EXECUTE FUNCTION control_plane.guard_managed_role_image_mapping();
GRANT SELECT,INSERT ON control_plane.managed_role_image_recipes,control_plane.managed_role_image_revisions,control_plane.managed_role_image_builds TO control_plane_runtime;
RESET ROLE;
