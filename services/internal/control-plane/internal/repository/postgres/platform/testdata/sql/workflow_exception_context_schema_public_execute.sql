-- Только DDL fault injection отдельной disposable clone, не бизнес-состояние.
GRANT EXECUTE ON FUNCTION control_plane.workflow_exception_context_node(uuid,uuid) TO PUBLIC;
