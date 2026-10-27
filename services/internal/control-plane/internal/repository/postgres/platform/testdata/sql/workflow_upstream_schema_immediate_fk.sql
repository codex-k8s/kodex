-- Только DDL отдельной disposable-копии; business state не подготавливается.
ALTER TABLE control_plane.run_nodes ALTER CONSTRAINT workflow_rework_gate_fk NOT DEFERRABLE;
