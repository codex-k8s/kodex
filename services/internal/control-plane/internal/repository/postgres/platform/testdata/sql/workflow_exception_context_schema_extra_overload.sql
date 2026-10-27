-- Только DDL fault injection отдельной disposable clone, не бизнес-состояние.
CREATE FUNCTION control_plane.workflow_exception_artifact_pins(uuid) RETURNS boolean
 LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$ SELECT false; $$;
