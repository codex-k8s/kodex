SELECT snapshot, action, snapshot_sha256, occurred_at
FROM control_plane.protected_resource_history
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND owner_actor_id = @actor_id::uuid
  AND resource_id = @resource_id::uuid
  AND resource_version < coalesce(nullif(@before_version, 0), 9007199254740991)
ORDER BY resource_version DESC
LIMIT @limit;
