-- +goose Up
SET ROLE control_plane_owner;
-- +goose StatementBegin
CREATE FUNCTION control_plane.catalog_resource_visible(
    tenant uuid, actor uuid, permission text, target_kind text, target_id uuid,
    target_project uuid, target_owner uuid, related_ids jsonb, evaluated_at timestamptz,
    include_project_membership boolean DEFAULT true
) RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path = pg_catalog, control_plane
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM control_plane.access_bindings binding
        JOIN control_plane.application_role_versions version ON version.id = binding.role_version_id
        JOIN control_plane.permission_registry definition ON definition.permission_key = permission
        JOIN control_plane.subjects subject ON subject.id = actor AND subject.organization_id = tenant AND subject.active
        WHERE binding.organization_id = tenant AND binding.state = 'ACTIVE'
          AND permission = ANY(version.permission_keys)
          AND target_kind = ANY(definition.resource_kinds)
          AND (include_project_membership OR binding.presentation_kind <> 'PROJECT_MEMBERSHIP')
          AND (binding.subject_id = actor OR binding.oidc_group_id IN (
              SELECT membership.group_id FROM control_plane.oidc_group_memberships membership
              WHERE membership.organization_id = tenant AND membership.subject_id = actor
          ))
          AND (binding.valid_from IS NULL OR binding.valid_from <= evaluated_at)
          AND (binding.valid_until IS NULL OR binding.valid_until > evaluated_at)
          AND (NOT binding.require_owner OR target_owner = actor)
          AND CASE binding.scope_kind
              WHEN 'ORGANIZATION' THEN true
              WHEN 'PROJECT' THEN binding.project_id = target_project
              WHEN 'RESOURCE_KIND' THEN binding.resource_kind = target_kind
                  AND (binding.project_id IS NULL OR binding.project_id = target_project)
              WHEN 'RESOURCE_INSTANCE' THEN binding.project_id IS NOT DISTINCT FROM target_project
                  AND ((binding.resource_kind = target_kind AND binding.resource_id = target_id)
                      OR related_ids ->> binding.resource_kind = binding.resource_id::text)
              ELSE false
          END
    );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.catalog_resource_visible(uuid, uuid, text, text, uuid, uuid, uuid, jsonb, timestamptz, boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.catalog_resource_visible(uuid, uuid, text, text, uuid, uuid, uuid, jsonb, timestamptz, boolean) TO control_plane_runtime;
CREATE VIEW control_plane.catalog_access_targets WITH (security_invoker = true) AS
SELECT project.organization_id, 'PROJECT'::text AS kind, project.ref, project.id,
       project.id AS project_id, project.created_by AS owner_id, '{}'::jsonb AS related_ids
FROM control_plane.projects project WHERE project.lifecycle = 'ACTIVE'
UNION ALL
SELECT agent.organization_id, 'AGENT', agent.ref, agent.id, agent.project_id, agent.created_by,
       jsonb_build_object('PROJECT', agent.project_id::text)
FROM control_plane.agents agent WHERE agent.project_id IS NOT NULL AND agent.state <> 'ARCHIVED'
UNION ALL
SELECT workflow.organization_id, 'WORKFLOW', workflow.ref, workflow.id, workflow.project_id, workflow.created_by,
       jsonb_build_object('PROJECT', workflow.project_id::text)
FROM control_plane.workflows workflow WHERE workflow.state <> 'ARCHIVED'
UNION ALL
SELECT run.organization_id, 'RUN', run.ref, run.id, run.project_id, run.initiated_by,
       jsonb_strip_nulls(jsonb_build_object('PROJECT', run.project_id::text, 'AGENT', agent.id::text, 'WORKFLOW', workflow.id::text))
FROM control_plane.runs run
LEFT JOIN control_plane.agents agent ON run.target_type = 'AGENT' AND agent.ref = run.target_ref AND agent.organization_id = run.organization_id
LEFT JOIN control_plane.workflows workflow ON run.target_type = 'WORKFLOW' AND workflow.ref = run.target_ref AND workflow.organization_id = run.organization_id
WHERE run.project_id IS NOT NULL OR run.target_type = 'SYSTEM_ASSISTANT'
UNION ALL
SELECT artifact.organization_id, 'ARTIFACT', artifact.ref, artifact.id, artifact.project_id, artifact.created_by,
       jsonb_strip_nulls(jsonb_build_object('PROJECT', artifact.project_id::text, 'RUN', run.id::text, 'AGENT', agent.id::text, 'WORKFLOW', workflow.id::text))
FROM control_plane.artifacts artifact
LEFT JOIN control_plane.runs run ON run.id = artifact.run_id AND run.organization_id = artifact.organization_id
LEFT JOIN control_plane.agents agent ON run.target_type = 'AGENT' AND agent.ref = run.target_ref AND agent.organization_id = run.organization_id
LEFT JOIN control_plane.workflows workflow ON run.target_type = 'WORKFLOW' AND workflow.ref = run.target_ref AND workflow.organization_id = run.organization_id
UNION ALL
SELECT schedule.organization_id, 'SCHEDULE', schedule.ref, schedule.id, schedule.project_id, schedule.created_by,
       jsonb_strip_nulls(jsonb_build_object('PROJECT', schedule.project_id::text, 'AGENT', agent.id::text, 'WORKFLOW', workflow.id::text))
FROM control_plane.schedules schedule
LEFT JOIN control_plane.agents agent ON schedule.target_type = 'AGENT' AND agent.ref = schedule.target_ref AND agent.organization_id = schedule.organization_id
LEFT JOIN control_plane.workflows workflow ON schedule.target_type = 'WORKFLOW' AND workflow.ref = schedule.target_ref AND workflow.organization_id = schedule.organization_id
WHERE schedule.lifecycle_state <> 'DELETED';
GRANT SELECT ON control_plane.catalog_access_targets TO control_plane_runtime;
RESET ROLE;
