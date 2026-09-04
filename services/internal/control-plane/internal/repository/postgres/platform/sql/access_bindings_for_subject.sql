-- name: access_bindings_for_subject :many
SELECT b.id::text, b.ref, b.version, b.state, b.subject_kind,
       COALESCE(binding_subject.ref, oidc_group.ref),
       COALESCE(binding_subject.display_name, oidc_group.display_name),
       COALESCE(binding_subject.active, oidc_group.state = 'ACTIVE'),
       role.ref, rv.ref, rv.revision, rv.name, rv.description, rv.permission_keys,
       rv.allowed_scopes, rv.change_comment, rv.created_at,
       creator.ref, creator.display_name, creator.email_masked,
       b.scope_kind, COALESCE(project.ref, ''), COALESCE(b.resource_kind, ''),
       CASE b.resource_kind
         WHEN 'PROJECT' THEN COALESCE((SELECT p.ref FROM control_plane.projects p WHERE p.id = b.resource_id), '')
         WHEN 'AGENT' THEN COALESCE((SELECT a.ref FROM control_plane.agents a WHERE a.id = b.resource_id), '')
         WHEN 'WORKFLOW' THEN COALESCE((SELECT w.ref FROM control_plane.workflows w WHERE w.id = b.resource_id), '')
         WHEN 'RUN' THEN COALESCE((SELECT run.ref FROM control_plane.runs run WHERE run.id = b.resource_id), '')
         WHEN 'OWNER_GATE' THEN COALESCE((SELECT gate.ref FROM control_plane.owner_gates gate WHERE gate.id = b.resource_id), '')
         WHEN 'ARTIFACT' THEN COALESCE((SELECT artifact.ref FROM control_plane.artifacts artifact WHERE artifact.id = b.resource_id), '')
         WHEN 'SCHEDULE' THEN COALESCE((SELECT schedule.ref FROM control_plane.schedules schedule WHERE schedule.id = b.resource_id), '')
         WHEN 'INTEGRATION' THEN COALESCE((SELECT connection.ref FROM control_plane.integration_connections connection WHERE connection.id = b.resource_id), '')
         WHEN 'SECRET' THEN COALESCE((SELECT secret.ref FROM control_plane.runtime_secrets secret WHERE secret.id = b.resource_id), '')
         ELSE ''
       END,
       b.valid_from, b.valid_until, b.require_owner, b.created_at, b.updated_at,
       b.presentation_kind
FROM control_plane.access_bindings b
JOIN control_plane.application_role_versions rv ON rv.id = b.role_version_id
JOIN control_plane.application_roles role ON role.id = rv.role_id
JOIN control_plane.subjects creator ON creator.id = rv.created_by
LEFT JOIN control_plane.subjects binding_subject ON binding_subject.id = b.subject_id
LEFT JOIN control_plane.oidc_groups oidc_group ON oidc_group.id = b.oidc_group_id
LEFT JOIN control_plane.projects project ON project.id = b.project_id
WHERE b.organization_id = @organization_id::uuid
  AND b.state = 'ACTIVE'
  AND (
    b.subject_id = NULLIF(@subject_id, '')::uuid OR
    b.oidc_group_id = NULLIF(@group_id, '')::uuid OR
    b.oidc_group_id IN (
      SELECT membership.group_id
      FROM control_plane.oidc_group_memberships membership
      WHERE membership.organization_id = b.organization_id
        AND membership.subject_id = NULLIF(@subject_id, '')::uuid
    )
  )
ORDER BY b.ref
