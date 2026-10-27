-- Только собственная disposable schema: отрицательная проверка настоящего release SQL.
DROP TRIGGER legacy_workflow_rework_guard ON control_plane.run_nodes;
CREATE TRIGGER legacy_workflow_rework_guard BEFORE UPDATE OF owner_rework_gate_id ON control_plane.run_nodes
FOR EACH ROW EXECUTE FUNCTION control_plane.guard_legacy_workflow_rework();
