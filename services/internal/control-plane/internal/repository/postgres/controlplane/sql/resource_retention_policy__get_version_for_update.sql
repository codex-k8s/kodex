-- name: ResourceRetentionPolicyGetVersionForUpdate :one
SELECT policy_id, version, pvc_retention_seconds, archive_retention_seconds,
       effective_at, coalesce(retired_at, 'epoch'::timestamptz)
FROM control_plane.resource_retention_policies
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND policy_id = @policy_id
  AND version = @version
FOR UPDATE;
