-- +goose Up
SET ROLE control_plane_owner;
-- +goose StatementBegin
CREATE FUNCTION control_plane.assistant_context_projection(
    tenant uuid, actor uuid, signed_project uuid, context_kind text, context_ref text, evaluated_at timestamptz,
    conversation_project uuid DEFAULT NULL
) RETURNS TABLE(project_id uuid, project_ref text, entity_name text, entity_version bigint, allowed_operations text[])
LANGUAGE sql STABLE SECURITY INVOKER SET search_path = pg_catalog, control_plane
AS $$
WITH resources AS (
    SELECT 'PROJECT'::text AS context_kind, target.kind, target.organization_id, target.ref, target.id,
           target.project_id, target.owner_id, target.related_ids, project.name, project.version, 'project.view'::text AS permission
    FROM control_plane.catalog_access_targets target
    JOIN control_plane.projects project ON target.kind='PROJECT' AND project.id=target.id
    UNION ALL
    SELECT 'AGENT', 'AGENT', agent.organization_id, agent.ref, agent.id, agent.project_id, agent.created_by,
           jsonb_build_object('PROJECT',agent.project_id::text), agent.name, agent.version, 'agent.view'
    FROM control_plane.agents agent WHERE agent.project_id IS NOT NULL
    UNION ALL
    SELECT 'WORKFLOW', 'WORKFLOW', workflow.organization_id, workflow.ref, workflow.id, workflow.project_id, workflow.created_by,
           jsonb_build_object('PROJECT',workflow.project_id::text), workflow.name, workflow.version, 'workflow.view'
    FROM control_plane.workflows workflow
    UNION ALL
    SELECT 'RUN', target.kind, target.organization_id, target.ref, target.id, target.project_id, target.owner_id,
           target.related_ids, run.title, run.version, 'run.view'
    FROM control_plane.catalog_access_targets target
    JOIN control_plane.runs run ON target.kind='RUN' AND run.id=target.id
    UNION ALL
    SELECT 'FILE', target.kind, target.organization_id, target.ref, target.id, target.project_id, target.owner_id,
           target.related_ids, artifact.file_name, artifact.version, 'artifact.view'
    FROM control_plane.catalog_access_targets target
    JOIN control_plane.artifacts artifact ON target.kind='ARTIFACT' AND artifact.id=target.id
    UNION ALL
    SELECT 'ENVIRONMENT', CASE WHEN environment.project_id IS NULL THEN 'ORGANIZATION' ELSE 'PROJECT' END,
           environment.organization_id, environment.ref, COALESCE(environment.project_id,environment.organization_id),
           environment.project_id, project.created_by, '{}'::jsonb, environment.name, environment.version,
           CASE WHEN environment.project_id IS NULL THEN 'organization.view' ELSE 'project.view' END
    FROM control_plane.runtime_environment_sets environment
    LEFT JOIN control_plane.projects project ON project.id=environment.project_id
    WHERE environment.state <> 'DELETED'
    UNION ALL
    SELECT 'INTEGRATION_CONNECTION', 'INTEGRATION', connection.organization_id, connection.ref, connection.id,
           NULL::uuid, connection.created_by, '{}'::jsonb, connection.name, connection.version, 'integration.view'
    FROM control_plane.integration_connections connection WHERE connection.lifecycle_state='ACTIVE'
    UNION ALL
    SELECT '', 'ORGANIZATION', organization.id, '', organization.id, NULL::uuid, NULL::uuid,
           '{}'::jsonb, '', NULL::bigint, 'organization.view'
    FROM control_plane.organizations organization
    WHERE conversation_project IS NULL
    UNION ALL
    SELECT '', 'PROJECT', project.organization_id, '', project.id, project.id, project.created_by,
           '{}'::jsonb, '', NULL::bigint, 'project.view'
    FROM control_plane.projects project WHERE project.id=conversation_project AND project.lifecycle='ACTIVE'
), eligible AS (
    SELECT resource.*, COALESCE(project.ref,'') AS project_ref
    FROM resources resource LEFT JOIN control_plane.projects project ON project.id=resource.project_id
    WHERE resource.organization_id=tenant AND resource.context_kind=assistant_context_projection.context_kind
      AND resource.ref=context_ref
      AND (resource.project_id IS NULL OR project.lifecycle='ACTIVE')
      AND (signed_project IS NULL OR resource.project_id IS NULL OR resource.project_id=signed_project)
      AND control_plane.catalog_resource_visible(tenant,actor,resource.permission,resource.kind,resource.id,
          resource.project_id,resource.owner_id,resource.related_ids,evaluated_at)
), operations(context_kind, operation, permission, ordinal) AS (VALUES
    ('','CREATE_PROJECT','project.create',1), ('','CREATE_INTEGRATION_CONNECTION','organization.manage',2),
    ('PROJECT','UPDATE_PROJECT','project.manage',1), ('PROJECT','CREATE_AGENT','project.manage',2),
    ('PROJECT','CREATE_WORKFLOW','project.manage',3), ('PROJECT','CREATE_SCHEDULE','project.manage',4),
    ('AGENT','CHANGE_CAPABILITY','agent.manage',1), ('AGENT','LAUNCH_RUN','agent.launch',2), ('AGENT','ARCHIVE_AGENT','agent.manage',3),
    ('WORKFLOW','LAUNCH_RUN','workflow.launch',1), ('WORKFLOW','ARCHIVE_WORKFLOW','workflow.manage',2),
    ('INTEGRATION_CONNECTION','TEST_INTEGRATION_CONNECTION','integration.manage',1)
)
SELECT eligible.project_id, eligible.project_ref, eligible.name, eligible.version,
       ARRAY(SELECT operation.operation FROM operations operation
             WHERE operation.context_kind=eligible.context_kind
               AND NOT EXISTS (SELECT 1 FROM control_plane.agents agent WHERE eligible.kind='AGENT' AND agent.id=eligible.id AND agent.state='ARCHIVED')
               AND NOT EXISTS (SELECT 1 FROM control_plane.workflows workflow WHERE eligible.kind='WORKFLOW' AND workflow.id=eligible.id AND workflow.state='ARCHIVED')
               AND control_plane.catalog_resource_visible(tenant,actor,operation.permission,eligible.kind,eligible.id,
                   eligible.project_id,eligible.owner_id,eligible.related_ids,evaluated_at)
             ORDER BY operation.ordinal)
       || CASE WHEN eligible.context_kind='INTEGRATION_CONNECTION' AND EXISTS (
            SELECT 1 FROM control_plane.integration_grant_admission(tenant,actor,signed_project,
                eligible.ref,'','','','','GRANT',NULL) admission WHERE admission.reason='READY'
          ) THEN ARRAY['CHANGE_INTEGRATION_GRANT'] ELSE '{}'::text[] END
       || CASE WHEN eligible.context_kind='PROJECT' AND EXISTS (
            SELECT 1 FROM control_plane.catalog_access_targets target
            WHERE target.organization_id=tenant AND target.project_id=eligible.project_id
              AND target.kind IN ('AGENT','WORKFLOW')
              AND control_plane.catalog_resource_visible(tenant,actor,
                  CASE target.kind WHEN 'AGENT' THEN 'agent.launch' ELSE 'workflow.launch' END,
                  target.kind,target.id,target.project_id,target.owner_id,target.related_ids,evaluated_at)
          ) THEN ARRAY['LAUNCH_RUN'] ELSE '{}'::text[] END
FROM eligible;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.assistant_context_projection(uuid,uuid,uuid,text,text,timestamptz,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.assistant_context_projection(uuid,uuid,uuid,text,text,timestamptz,uuid) TO control_plane_runtime;
RESET ROLE;
