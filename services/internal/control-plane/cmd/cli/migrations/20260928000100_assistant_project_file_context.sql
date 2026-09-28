-- +goose Up
-- Проектный контекст разрешает помощнику подготовить текстовый файл только
-- при наличии точного artifact.upload на этом Проекте.
SET ROLE control_plane_owner;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.assistant_context_projection_v2(
    tenant uuid, actor uuid, signed_project uuid, requested_context_kind text,
    requested_context_ref text, evaluated_at timestamptz, conversation_project uuid DEFAULT NULL
) RETURNS TABLE(project_id uuid, project_ref text, entity_name text, entity_version bigint, allowed_operations text[])
LANGUAGE sql STABLE SECURITY INVOKER SET search_path = pg_catalog, control_plane
AS $$
SELECT projection.project_id, projection.project_ref, projection.entity_name, projection.entity_version,
       projection.allowed_operations || CASE
         WHEN requested_context_kind='PROJECT' AND EXISTS (
           SELECT 1
           FROM control_plane.catalog_access_targets target
           WHERE target.organization_id=tenant
             AND target.kind='PROJECT'
             AND target.id=projection.project_id
             AND control_plane.catalog_resource_visible(
               tenant,actor,'artifact.upload',target.kind,target.id,target.project_id,
               target.owner_id,target.related_ids,evaluated_at
             )
         ) THEN ARRAY['CREATE_PROJECT_FILE']
         ELSE '{}'::text[]
       END
FROM control_plane.assistant_context_projection(
  tenant,actor,signed_project,requested_context_kind,requested_context_ref,evaluated_at,conversation_project
) projection;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.assistant_context_projection_v2(uuid,uuid,uuid,text,text,timestamptz,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.assistant_context_projection_v2(uuid,uuid,uuid,text,text,timestamptz,uuid) TO control_plane_runtime;
RESET ROLE;
