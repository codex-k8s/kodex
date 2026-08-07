SELECT snapshot, action, snapshot_sha256, occurred_at
FROM control_plane.protected_resource_history
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND resource_id = @resource_id::uuid
  AND resource_kind = 'INSTRUCTION_SET'
  AND (snapshot -> 'spec' ->> 'currentVersion')::bigint = @content_version
ORDER BY resource_version DESC
LIMIT 1;
