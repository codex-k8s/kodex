-- Только собственная disposable schema: отрицательная проверка настоящего release SQL.
ALTER TABLE control_plane.run_nodes DROP CONSTRAINT legacy_rework_origin_requires_gate;
