-- Только DDL отдельной disposable-копии; business state не подготавливается.
ALTER TABLE control_plane.owner_gates DROP CONSTRAINT owner_gate_closure_kind;
