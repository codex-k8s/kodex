UPDATE control_plane.legacy_graph_migration_plans
SET state = @state,
    verification_state = @verification_state,
    terminal_at = @terminal_at
WHERE plan_id = @plan_id::uuid
  AND state = 'PREPARED'
