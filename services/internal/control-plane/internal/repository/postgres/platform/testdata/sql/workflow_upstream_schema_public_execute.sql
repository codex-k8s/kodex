-- Только DDL отдельной disposable-копии; business state не подготавливается.
GRANT EXECUTE ON FUNCTION control_plane.workflow_effective_inputs(uuid,uuid,text) TO PUBLIC;
