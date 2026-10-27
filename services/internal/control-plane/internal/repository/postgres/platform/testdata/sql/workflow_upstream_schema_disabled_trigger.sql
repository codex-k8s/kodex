-- Только DDL отдельной disposable-копии; business state не подготавливается.
ALTER TABLE control_plane.run_nodes DISABLE TRIGGER workflow_rework_node_guard;
