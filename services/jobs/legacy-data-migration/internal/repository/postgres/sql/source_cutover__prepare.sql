-- name: source_cutover__prepare :one
INSERT INTO matter_codex_legacy_data_cutovers (
    plan_id, plan_sha256, source_sha256, target_sha256,
    backup_sha256, manifest_sha256, materialization_sha256,
    materialization_count, state, prepared_at
) VALUES (
    @plan_id, @plan_sha256, @source_sha256, @target_sha256,
    @backup_sha256, @manifest_sha256, @materialization_sha256,
    @materialization_count, 'PREPARED', transaction_timestamp()
)
ON CONFLICT (plan_id) DO UPDATE
SET plan_id = EXCLUDED.plan_id
WHERE matter_codex_legacy_data_cutovers.plan_sha256 = EXCLUDED.plan_sha256
  AND matter_codex_legacy_data_cutovers.source_sha256 = EXCLUDED.source_sha256
  AND matter_codex_legacy_data_cutovers.target_sha256 = EXCLUDED.target_sha256
  AND matter_codex_legacy_data_cutovers.backup_sha256 = EXCLUDED.backup_sha256
  AND matter_codex_legacy_data_cutovers.manifest_sha256 = EXCLUDED.manifest_sha256
  AND matter_codex_legacy_data_cutovers.materialization_sha256 = EXCLUDED.materialization_sha256
  AND matter_codex_legacy_data_cutovers.materialization_count = EXCLUDED.materialization_count
  AND matter_codex_legacy_data_cutovers.state = 'PREPARED'
RETURNING plan_id, plan_sha256, source_sha256, target_sha256,
          backup_sha256, manifest_sha256, materialization_sha256,
          materialization_count, state, false;
