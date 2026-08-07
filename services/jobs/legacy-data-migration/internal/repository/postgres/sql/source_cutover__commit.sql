-- name: source_cutover__commit :one
UPDATE matter_codex_legacy_data_cutovers
SET state = 'COMMITTED', committed_at = coalesce(committed_at, transaction_timestamp())
WHERE plan_id = @plan_id
  AND plan_sha256 = @plan_sha256
  AND source_sha256 = @source_sha256
  AND target_sha256 = @target_sha256
  AND backup_sha256 = @backup_sha256
  AND manifest_sha256 = @manifest_sha256
  AND state IN ('FROZEN', 'COMMITTED')
RETURNING plan_id, plan_sha256, source_sha256, target_sha256,
          backup_sha256, manifest_sha256, state, false;
