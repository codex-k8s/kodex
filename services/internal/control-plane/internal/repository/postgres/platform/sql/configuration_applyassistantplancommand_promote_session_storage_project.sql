-- name: configuration_applyassistantplancommand_promote_session_storage_project :exec
UPDATE control_plane.session_storage storage
SET project_id = $1::uuid,
    version = storage.version + 1,
    updated_at = clock_timestamp()
WHERE storage.session_id = $2::uuid
  AND storage.organization_id = $3::uuid
  AND storage.project_id IS NULL;
