-- Только fault injection в отдельной disposable schema; production данные не изменяются.
CREATE OR REPLACE FUNCTION control_plane.workflow_evidence_execution_assigned(uuid,uuid,uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$ SELECT true $$;
