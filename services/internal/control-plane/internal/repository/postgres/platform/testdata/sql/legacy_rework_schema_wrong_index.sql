-- Только собственная disposable schema: отрицательная проверка настоящего release SQL.
DROP INDEX control_plane.run_nodes_owner_rework_once;
CREATE UNIQUE INDEX run_nodes_owner_rework_once ON control_plane.run_nodes(owner_rework_gate_id) WHERE owner_rework_gate_id IS NOT NULL;
