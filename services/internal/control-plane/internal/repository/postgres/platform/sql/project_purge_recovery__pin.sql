-- name: project_purge_recovery__pin :one
SELECT control_plane.project_purge_recovery_pin(@id::uuid,@owner,@generation,@storage_identity,@object_version,@object_etag);
