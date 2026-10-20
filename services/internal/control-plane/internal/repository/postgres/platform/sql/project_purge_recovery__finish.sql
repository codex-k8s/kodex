-- name: project_purge_recovery__finish :one
SELECT control_plane.project_purge_recovery_finish(@id::uuid,@owner,@generation,@storage_identity,@outcome);
