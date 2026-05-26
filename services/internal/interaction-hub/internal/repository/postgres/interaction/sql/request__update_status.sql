-- name: request__update_status :exec
UPDATE interaction_hub_requests
SET status = @status,
    version = @version,
    updated_at = @updated_at,
    resolved_at = @resolved_at
WHERE id = @id
  AND version = @previous_version;
