CREATE OR REPLACE FUNCTION control_plane.purge_project_database(p_organization_id uuid,p_project_id uuid,p_objects_digest text)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
BEGIN
 RETURN 0;
END;
$$;
