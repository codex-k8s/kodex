-- name: ResourceRetentionPolicyRetire :exec
UPDATE control_plane.resource_retention_policies
SET retired_at = @retired_at
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND policy_id = @policy_id
  AND version = @version
  AND retired_at IS NULL;
