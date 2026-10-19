-- name: project_purge_recovery__resolve :one
SELECT control_plane.project_purge_recovery_resolve(@organization_id::uuid,@project_ref,@expected_version,
 @snapshot_digest,@actor_id::uuid,@resolution_ref,@storage_identity);
