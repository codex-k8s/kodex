INSERT INTO control_plane.legacy_graph_source_dispositions (
    plan_id, source_table, disposition, row_count, source_sha256,
    terminal_state_sha256
) VALUES (
    @plan_id::uuid, @source_table, @disposition, @row_count,
    @source_sha256, nullif(@terminal_state_sha256, '')
)
