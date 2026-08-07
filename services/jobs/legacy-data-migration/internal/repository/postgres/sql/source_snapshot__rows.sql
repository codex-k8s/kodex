-- name: source_snapshot__rows :many
SELECT table_name, row_payload
FROM matter_codex_legacy_snapshot_rows();
