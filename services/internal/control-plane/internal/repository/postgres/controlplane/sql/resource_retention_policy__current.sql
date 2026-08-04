-- name: ResourceRetentionPolicyCurrent :one
SELECT policy_id, version, pvc_retention_seconds, archive_retention_seconds, effective_at
FROM control_plane.resource_retention_policies
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND retired_at IS NULL
  AND effective_at <= current_timestamp
ORDER BY version DESC
LIMIT 1
FOR SHARE;
