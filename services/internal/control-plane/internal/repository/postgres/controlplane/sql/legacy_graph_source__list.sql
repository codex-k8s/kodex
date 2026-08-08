-- name: ListLegacySourceDispositions :many
SELECT plan_id::text, source_table, disposition, row_count,
       source_sha256, coalesce(terminal_state_sha256, '')
FROM control_plane.legacy_graph_source_dispositions
WHERE plan_id = @plan_id::uuid
ORDER BY source_table
