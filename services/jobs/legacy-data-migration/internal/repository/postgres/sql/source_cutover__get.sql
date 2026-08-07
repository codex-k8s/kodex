-- name: source_cutover__get :one
SELECT plan_id, plan_sha256, source_sha256, target_sha256,
       backup_sha256, manifest_sha256, state, false
FROM matter_codex_legacy_data_cutovers
WHERE plan_id = @plan_id;
