-- Только DDL fault injection отдельной disposable clone, не бизнес-состояние.
ALTER FUNCTION control_plane.workflow_exception_context_node(uuid,uuid) SET search_path TO public;
