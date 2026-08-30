-- name: access_resolve_target :one
SELECT resource_id, project_id, project_ref, owner_ref, related_refs
FROM (
  SELECT o.id::text AS resource_id, '' AS project_id, '' AS project_ref,
         '' AS owner_ref, '{}'::jsonb AS related_refs
  FROM control_plane.organizations o
  WHERE @resource_kind = 'ORGANIZATION' AND o.id = @organization_id::uuid
  UNION ALL
  SELECT p.id::text, p.id::text, p.ref, owner_subject.ref, '{}'::jsonb
  FROM control_plane.projects p
  JOIN control_plane.subjects owner_subject ON owner_subject.id = p.created_by
  WHERE @resource_kind = 'PROJECT' AND p.organization_id = @organization_id::uuid
    AND p.ref = @resource_ref AND p.lifecycle = 'ACTIVE'
  UNION ALL
  SELECT a.id::text, p.id::text, p.ref, owner_subject.ref,
         jsonb_build_object('PROJECT', p.ref)
  FROM control_plane.agents a
  JOIN control_plane.projects p ON p.id = a.project_id
  JOIN control_plane.subjects owner_subject ON owner_subject.id = a.created_by
  WHERE @resource_kind = 'AGENT' AND a.organization_id = @organization_id::uuid
    AND a.ref = @resource_ref AND a.project_id IS NOT NULL AND a.state <> 'ARCHIVED'
  UNION ALL
  SELECT w.id::text, p.id::text, p.ref, owner_subject.ref,
         jsonb_build_object('PROJECT', p.ref)
  FROM control_plane.workflows w
  JOIN control_plane.projects p ON p.id = w.project_id
  JOIN control_plane.subjects owner_subject ON owner_subject.id = w.created_by
  WHERE @resource_kind = 'WORKFLOW' AND w.organization_id = @organization_id::uuid
    AND w.ref = @resource_ref AND w.state <> 'ARCHIVED'
  UNION ALL
  SELECT run.id::text, p.id::text, p.ref, owner_subject.ref,
         jsonb_strip_nulls(jsonb_build_object(
           'PROJECT', p.ref,
           'AGENT', CASE WHEN run.target_type = 'AGENT' THEN run.target_ref END,
           'WORKFLOW', CASE WHEN run.target_type = 'WORKFLOW' THEN run.target_ref END
         ))
  FROM control_plane.runs run
  JOIN control_plane.projects p ON p.id = run.project_id
  JOIN control_plane.subjects owner_subject ON owner_subject.id = run.initiated_by
  WHERE @resource_kind = 'RUN' AND run.organization_id = @organization_id::uuid
    AND run.ref = @resource_ref
  UNION ALL
  SELECT session.id::text, COALESCE(project.id::text, ''), COALESCE(project.ref, ''), owner_subject.ref,
         jsonb_strip_nulls(jsonb_build_object(
           'PROJECT', project.ref,
           'AGENT', CASE WHEN session.target_type = 'AGENT' THEN session.target_ref END,
           'WORKFLOW', CASE WHEN session.target_type = 'WORKFLOW' THEN session.target_ref END
         ))
  FROM control_plane.sessions session
  LEFT JOIN control_plane.projects project ON project.id = session.project_id
  JOIN control_plane.subjects owner_subject ON owner_subject.id = session.created_by
  WHERE @resource_kind = 'SESSION' AND session.organization_id = @organization_id::uuid
    AND session.ref = @resource_ref
  UNION ALL
  SELECT gate.id::text, p.id::text, p.ref, owner_subject.ref,
         jsonb_build_object('PROJECT', p.ref, 'RUN', run.ref)
  FROM control_plane.owner_gates gate
  JOIN control_plane.projects p ON p.id = gate.project_id
  JOIN control_plane.runs run ON run.id = gate.root_run_id
  JOIN control_plane.subjects owner_subject ON owner_subject.id = run.initiated_by
  WHERE @resource_kind = 'OWNER_GATE' AND gate.organization_id = @organization_id::uuid
    AND gate.ref = @resource_ref
  UNION ALL
  SELECT artifact.id::text, COALESCE(p.id::text, ''), COALESCE(p.ref, ''), owner_subject.ref,
         jsonb_strip_nulls(jsonb_build_object('PROJECT', p.ref, 'RUN', run.ref,
           'AGENT', CASE WHEN run.target_type = 'AGENT' THEN run.target_ref END,
           'WORKFLOW', CASE WHEN run.target_type = 'WORKFLOW' THEN run.target_ref END))
  FROM control_plane.artifacts artifact
  LEFT JOIN control_plane.projects p ON p.id = artifact.project_id
  JOIN control_plane.subjects owner_subject ON owner_subject.id = artifact.created_by
  LEFT JOIN control_plane.runs run ON run.id = artifact.run_id
  WHERE @resource_kind = 'ARTIFACT' AND artifact.organization_id = @organization_id::uuid
    AND artifact.ref = @resource_ref
  UNION ALL
  SELECT schedule.id::text, p.id::text, p.ref, owner_subject.ref,
         jsonb_strip_nulls(jsonb_build_object('PROJECT', p.ref,
           'AGENT', CASE WHEN schedule.target_type = 'AGENT' THEN schedule.target_ref END,
           'WORKFLOW', CASE WHEN schedule.target_type = 'WORKFLOW' THEN schedule.target_ref END))
  FROM control_plane.schedules schedule
  JOIN control_plane.projects p ON p.id = schedule.project_id
  JOIN control_plane.subjects owner_subject ON owner_subject.id = schedule.created_by
  WHERE @resource_kind = 'SCHEDULE' AND schedule.organization_id = @organization_id::uuid
    AND schedule.ref = @resource_ref
  UNION ALL
  SELECT connection.id::text, '' AS project_id, '' AS project_ref, owner_subject.ref,
         '{}'::jsonb
  FROM control_plane.integration_connections connection
  JOIN control_plane.subjects owner_subject ON owner_subject.id = connection.created_by
  WHERE @resource_kind = 'INTEGRATION' AND connection.organization_id = @organization_id::uuid
    AND connection.ref = @resource_ref
  UNION ALL
  SELECT recipe.id::text, project.id::text, project.ref, owner_subject.ref,
         jsonb_build_object('PROJECT', project.ref)
  FROM control_plane.role_image_recipes recipe
  JOIN control_plane.projects project ON project.id = recipe.project_id
  JOIN control_plane.subjects owner_subject ON owner_subject.id = recipe.created_by
  WHERE @resource_kind = 'ROLE_IMAGE' AND recipe.organization_id = @organization_id::uuid
    AND recipe.ref = @resource_ref AND recipe.state = 'ACTIVE'
  UNION ALL
  SELECT environment.id::text, project.id::text, project.ref, owner_subject.ref,
         jsonb_build_object('PROJECT', project.ref)
  FROM control_plane.runtime_environment_sets environment
  JOIN control_plane.projects project ON project.id = environment.project_id
  JOIN control_plane.subjects owner_subject ON owner_subject.id = environment.created_by
  WHERE @resource_kind = 'RUNTIME_ENVIRONMENT' AND environment.organization_id = @organization_id::uuid
    AND environment.ref = @resource_ref AND environment.state = 'ACTIVE'
  UNION ALL
  SELECT secret.id::text, project.id::text, project.ref, owner_subject.ref,
         jsonb_build_object('PROJECT', project.ref)
  FROM control_plane.runtime_secrets secret
  JOIN control_plane.projects project ON project.id = secret.project_id
  JOIN control_plane.subjects owner_subject ON owner_subject.id = secret.created_by
  WHERE @resource_kind = 'SECRET' AND secret.organization_id = @organization_id::uuid
    AND secret.ref = @resource_ref AND secret.state <> 'PROVISIONING'
) resolved
LIMIT 1
