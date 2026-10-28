-- Только DDL fault injection отдельной disposable clone, не бизнес-состояние.
CREATE OR REPLACE FUNCTION control_plane.workflow_exception_artifact_pins(p_tenant uuid,p_node uuid)
RETURNS TABLE(artifact_id uuid,revision_id uuid) LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT NULL::uuid,NULL::uuid WHERE false;
$$;
