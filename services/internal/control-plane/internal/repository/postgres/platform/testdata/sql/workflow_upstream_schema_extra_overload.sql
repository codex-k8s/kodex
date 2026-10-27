-- Только DDL отдельной disposable-копии; business state не подготавливается.
CREATE FUNCTION control_plane.workflow_effective_inputs(uuid,uuid) RETURNS jsonb LANGUAGE sql STABLE AS $$ SELECT NULL::jsonb; $$;
