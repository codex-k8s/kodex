-- name: flow__update :exec
UPDATE agent_manager_flows
SET
    display_name = @display_name,
    description = @description,
    icon_object_uri = @icon_object_uri,
    status = @status,
    active_version_id = @active_version_id::uuid,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
