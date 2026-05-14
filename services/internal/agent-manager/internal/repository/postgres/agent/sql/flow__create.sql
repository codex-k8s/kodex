-- name: flow__create :exec
INSERT INTO agent_manager_flows (
    id, scope_type, scope_ref, slug, display_name, description, icon_object_uri,
    status, active_version_id, version, created_at, updated_at
) VALUES (
    @id, @scope_type, @scope_ref, @slug, @display_name, @description, @icon_object_uri,
    @status, @active_version_id::uuid, @version, @created_at, @updated_at
);
