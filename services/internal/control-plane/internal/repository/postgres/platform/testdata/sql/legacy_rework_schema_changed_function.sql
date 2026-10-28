-- Только собственная disposable schema: отрицательная проверка настоящего release SQL.
CREATE OR REPLACE FUNCTION control_plane.legacy_rework_execution_assigned(p_organization uuid,p_node uuid) RETURNS boolean
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$ SELECT false; $$;
