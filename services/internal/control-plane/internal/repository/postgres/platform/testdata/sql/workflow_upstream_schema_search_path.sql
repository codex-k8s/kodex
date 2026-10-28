-- Только DDL отдельной disposable-копии; business state не подготавливается.
ALTER FUNCTION control_plane.workflow_effective_inputs(uuid,uuid,text) SET search_path=pg_catalog;
