-- name: legacy_policy_function :exec
CREATE FUNCTION control_plane.integration_policy_legacy_capabilities(p_key text,p_current_version text,p_current_digest text,p_version text,p_digest text) RETURNS jsonb LANGUAGE sql IMMUTABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $history$
SELECT CASE
{{range .}} WHEN (p_key,p_current_version,p_current_digest,p_version,p_digest)=('{{.Key}}','{{.CurrentVersion}}','{{.CurrentDigest}}','{{.Version}}','{{.Digest}}') THEN $caps${{.Capabilities}}$caps$::jsonb
{{end}} ELSE NULL::jsonb END;
$history$;
