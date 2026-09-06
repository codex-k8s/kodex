-- +goose Up
SET ROLE control_plane_owner;

INSERT INTO control_plane.permission_registry
 (permission_key,name_key,description_key,risk,allowed_scopes,resource_kinds,owner_condition_supported)
VALUES
 ('image.source.view','i18n:PERMISSION_IMAGE_SOURCE_VIEW_NAME','i18n:PERMISSION_IMAGE_SOURCE_VIEW_DESCRIPTION','ADMIN',
  ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'],ARRAY['ORGANIZATION','PROJECT','ROLE_IMAGE'],false),
 ('image.source.manage','i18n:PERMISSION_IMAGE_SOURCE_MANAGE_NAME','i18n:PERMISSION_IMAGE_SOURCE_MANAGE_DESCRIPTION','ADMIN',
  ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'],ARRAY['ORGANIZATION','PROJECT','ROLE_IMAGE'],false);

-- Существующие системные роли переходят на новую immutable revision;
-- пользовательские роли не получают доступ к исходному тексту автоматически.
-- +goose StatementBegin
DO $$
DECLARE
 item record;
 next_id uuid;
 keys text[];
BEGIN
 FOR item IN
  SELECT role.id,role.organization_id,role.current_version_id,version.revision,
   version.name,version.description,version.permission_keys,version.allowed_scopes,version.created_by
  FROM control_plane.application_roles role
  JOIN control_plane.application_role_versions version ON version.id=role.current_version_id
  WHERE role.stable_key IN ('OWNER','ADMINISTRATOR')
 LOOP
  SELECT array_agg(DISTINCT key ORDER BY key) INTO keys
  FROM unnest(item.permission_keys || ARRAY['image.source.view','image.source.manage']) key;
  INSERT INTO control_plane.application_role_versions
   (ref,organization_id,role_id,revision,name,description,permission_keys,allowed_scopes,change_comment,created_by)
  VALUES ('arv_image_source_' || substr(md5(item.id::text),1,16),item.organization_id,item.id,item.revision+1,
   item.name,item.description,keys,item.allowed_scopes,'i18n:SYSTEM_ROLE_IMAGE_SOURCE_ACCESS',item.created_by)
  RETURNING id INTO next_id;
  UPDATE control_plane.application_roles SET current_version_id=next_id,version=version+1,updated_at=clock_timestamp() WHERE id=item.id;
  UPDATE control_plane.access_bindings SET role_version_id=next_id,version=version+1,updated_at=clock_timestamp()
   WHERE role_version_id=item.current_version_id AND state='ACTIVE';
 END LOOP;
END $$;
-- +goose StatementEnd
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Role image source permissions are forward-only'; END $$;
-- +goose StatementEnd
