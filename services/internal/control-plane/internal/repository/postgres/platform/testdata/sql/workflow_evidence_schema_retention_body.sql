-- Только fault injection отдельной disposable schema.
CREATE OR REPLACE FUNCTION control_plane.artifact_has_retained_revisions(uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$SELECT false$$;
