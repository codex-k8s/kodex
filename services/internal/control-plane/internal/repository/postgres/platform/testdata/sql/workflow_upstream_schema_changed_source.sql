-- Только DDL отдельной disposable-копии; business state не подготавливается.
CREATE OR REPLACE FUNCTION control_plane.workflow_effective_inputs(p_organization uuid,p_root uuid,p_step text) RETURNS jsonb LANGUAGE plpgsql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$ BEGIN RETURN '{}'::jsonb; END; $$;
