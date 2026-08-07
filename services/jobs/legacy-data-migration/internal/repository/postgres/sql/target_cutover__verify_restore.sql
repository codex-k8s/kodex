-- name: target_cutover__verify_restore :one
SELECT plan_id, plan_sha256, source_sha256, target_sha256,
       backup_sha256, manifest_sha256, materialization_sha256,
       materialization_count, state, restore_verified
FROM control_plane.verify_legacy_data_cutover_restore(
    @plan_id, @plan_sha256, @source_sha256, @target_sha256,
    @backup_sha256, @manifest_sha256, @materialization_sha256,
    @materialization_count
);
