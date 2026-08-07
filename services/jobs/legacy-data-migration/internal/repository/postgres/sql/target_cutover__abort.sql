-- name: target_cutover__abort :one
SELECT plan_id, plan_sha256, source_sha256, target_sha256,
       backup_sha256, manifest_sha256, materialization_sha256,
       materialization_count, state, restore_verified
FROM control_plane.abort_legacy_data_cutover(
    @plan_id, @plan_sha256, @source_sha256, @target_sha256,
    @backup_sha256, @manifest_sha256, @materialization_sha256,
    @materialization_count
);
