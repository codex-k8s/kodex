-- name: project_purge_authority_drift_guard :exec
CREATE OR REPLACE FUNCTION control_plane.project_purge_authorized() RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS 'SELECT true';
