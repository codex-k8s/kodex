-- name: ProviderBindingActiveSessions
SELECT count(*)::bigint
FROM control_plane.resources
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND kind = 'SESSION'
  AND state NOT IN ('ARCHIVED', 'CANCELLED', 'DELETION_PENDING', 'DELETED')
  AND spec ->> 'providerAccountBindingId' = @binding_id
