-- name: access_insert_owner_binding :exec
INSERT INTO control_plane.access_bindings
    (ref, organization_id, subject_kind, subject_id, role_version_id, scope_kind,
     presentation_kind, created_by)
SELECT @ref, @organization_id::uuid, 'USER', @subject_id::uuid, rv.id, 'ORGANIZATION',
       'PLATFORM_MEMBERSHIP', @subject_id::uuid
FROM control_plane.application_roles r
JOIN control_plane.application_role_versions rv ON rv.id = r.current_version_id
WHERE r.organization_id = @organization_id::uuid AND r.kind = 'SYSTEM' AND r.stable_key = 'OWNER'
  AND NOT EXISTS (
    SELECT 1 FROM control_plane.access_bindings b
    WHERE b.organization_id = r.organization_id AND b.subject_id = @subject_id::uuid
      AND b.presentation_kind = 'PLATFORM_MEMBERSHIP' AND b.state = 'ACTIVE'
  )
