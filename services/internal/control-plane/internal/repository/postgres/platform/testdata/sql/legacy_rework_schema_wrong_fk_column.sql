-- Только собственная disposable schema: отрицательная проверка настоящего release SQL.
ALTER TABLE control_plane.run_nodes DROP CONSTRAINT legacy_rework_gate_fk;
ALTER TABLE control_plane.run_nodes ADD CONSTRAINT legacy_rework_gate_fk FOREIGN KEY(owner_rework_origin_node_id)
REFERENCES control_plane.owner_gates(id) DEFERRABLE INITIALLY IMMEDIATE;
