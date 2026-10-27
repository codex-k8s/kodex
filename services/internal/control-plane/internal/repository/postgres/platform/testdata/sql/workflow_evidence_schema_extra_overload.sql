-- Fault injection: overload не должен скрываться за exact signature lookup.
CREATE FUNCTION control_plane.workflow_evidence_outcome_valid(uuid,uuid,jsonb) RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$ SELECT true $$;
