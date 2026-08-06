INSERT INTO control_plane.protected_resource_history (
    organization_id, project_id, resource_id, resource_version,
    resource_kind, owner_actor_id, action, snapshot, snapshot_sha256,
    occurred_at
) VALUES (
    @organization_id::uuid, @project_id::uuid, @resource_id::uuid,
    @resource_version, @resource_kind, @owner_actor_id::uuid, @action,
    @snapshot::jsonb, @snapshot_sha256, @occurred_at
);
