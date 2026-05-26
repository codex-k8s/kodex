-- name: thread__update_latest_message :exec
UPDATE interaction_hub_threads
SET
    status = @status,
    latest_message_id = @latest_message_id::uuid,
    version = @version,
    updated_at = @updated_at,
    closed_at = @closed_at
WHERE id = @id
  AND version = @previous_version;
