-- +goose Up
SET ROLE control_plane_owner;

INSERT INTO control_plane.permission_registry(permission_key,name_key,description_key,risk,allowed_scopes,resource_kinds,owner_condition_supported)
VALUES('artifact.revision.create','i18n:PERMISSION_ARTIFACT_REVISION_CREATE_NAME','i18n:PERMISSION_ARTIFACT_REVISION_CREATE_DESCRIPTION','WRITE',
 ARRAY['ORGANIZATION','PROJECT','RESOURCE_KIND','RESOURCE_INSTANCE'],ARRAY['ARTIFACT'],false);

-- Default roles получают новую явную permission через новую immutable version.
-- Custom roles и прежние upload-only grants автоматически не расширяются.
-- +goose StatementBegin
DO $$
DECLARE role_row record; next_id uuid; next_permissions text[];
BEGIN
 FOR role_row IN SELECT role.id,role.organization_id,role.current_version_id,version.revision,
   version.name,version.description,version.permission_keys,version.allowed_scopes,version.created_by
 FROM control_plane.application_roles role JOIN control_plane.application_role_versions version ON version.id=role.current_version_id
 WHERE role.stable_key IN ('OWNER','ADMINISTRATOR')
 LOOP
  next_permissions:=ARRAY(SELECT DISTINCT permission_key FROM unnest(role_row.permission_keys || ARRAY['artifact.revision.create']) permission_key ORDER BY permission_key);
  INSERT INTO control_plane.application_role_versions(ref,organization_id,role_id,revision,name,description,permission_keys,allowed_scopes,change_comment,created_by)
  VALUES('arv_file_revision_'||substr(md5(role_row.id::text||role_row.revision::text),1,20),role_row.organization_id,role_row.id,role_row.revision+1,
   role_row.name,role_row.description,next_permissions,role_row.allowed_scopes,'i18n:SYSTEM_ROLE_ARTIFACT_REVISION_AUTHORITY',role_row.created_by)
  RETURNING id INTO next_id;
  UPDATE control_plane.application_roles SET current_version_id=next_id,version=version+1,updated_at=clock_timestamp() WHERE id=role_row.id;
  UPDATE control_plane.access_bindings SET role_version_id=next_id,version=version+1,updated_at=clock_timestamp()
   WHERE role_version_id=role_row.current_version_id AND state='ACTIVE';
 END LOOP;
END $$;
-- +goose StatementEnd

-- v3 сохраняет прежние screen gates. Только новая specialized revision-create
-- добавляет PROJECT-global границу; helper scope передаёт server-resolved caller.
-- +goose StatementBegin
CREATE FUNCTION control_plane.assistant_context_projection_v3(
 tenant uuid,actor uuid,signed_project uuid,requested_context_kind text,requested_context_ref text,
 evaluated_at timestamptz,conversation_project uuid DEFAULT NULL,helper_scope text DEFAULT 'SYSTEM'
) RETURNS TABLE(project_id uuid,project_ref text,entity_name text,entity_version bigint,allowed_operations text[])
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
 SELECT projection.project_id,projection.project_ref,projection.entity_name,projection.entity_version,
  projection.allowed_operations || CASE WHEN conversation_project IS NOT NULL
   AND (helper_scope='PROJECT' OR (helper_scope='SYSTEM' AND requested_context_kind IN ('PROJECT','FILE')))
   AND (projection.project_id=conversation_project OR helper_scope='PROJECT' AND projection.project_id IS NULL)
   AND EXISTS(SELECT 1 FROM control_plane.artifacts artifact
    WHERE artifact.organization_id=tenant AND artifact.project_id=conversation_project AND artifact.lifecycle_state='ACTIVE' AND artifact.scan_state='CLEAN'
     AND control_plane.catalog_resource_visible(tenant,actor,'artifact.view','ARTIFACT',artifact.id,artifact.project_id,artifact.created_by,'{}'::jsonb,evaluated_at)
     AND control_plane.catalog_resource_visible(tenant,actor,'artifact.download','ARTIFACT',artifact.id,artifact.project_id,artifact.created_by,'{}'::jsonb,evaluated_at)
     AND control_plane.catalog_resource_visible(tenant,actor,'artifact.revision.create','ARTIFACT',artifact.id,artifact.project_id,artifact.created_by,'{}'::jsonb,evaluated_at))
   THEN ARRAY['CREATE_PROJECT_FILE_REVISION'] ELSE '{}'::text[] END
 FROM control_plane.assistant_context_projection_v2(tenant,actor,signed_project,requested_context_kind,requested_context_ref,evaluated_at,conversation_project) projection;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.assistant_context_projection_v3(uuid,uuid,uuid,text,text,timestamptz,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.assistant_context_projection_v3(uuid,uuid,uuid,text,text,timestamptz,uuid,text) TO control_plane_runtime;

-- Старое durable raw-content предложение несовместимо со staging boundary.
-- Applied history и её receipts не переписываются; новый proposal обязателен.
UPDATE control_plane.assistant_plans plan SET state='STALE',validated_revision=NULL,
 validation_problems=ARRAY['i18n:ASSISTANT_PLAN_CONTENT_STAGING_REQUIRED'],version=version+1
WHERE plan.state IN ('DRAFT','VALID','INVALID','STALE') AND EXISTS(
 SELECT 1 FROM jsonb_array_elements(plan.operations) operation
 WHERE operation->>'type'='CREATE_PROJECT_FILE' AND operation->'parameters' ? 'content');

RESET ROLE;
