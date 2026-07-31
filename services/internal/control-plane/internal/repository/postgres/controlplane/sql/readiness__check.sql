-- name: ReadinessRuntimeIdentity
SELECT
    identity.schema_version,
    pg_has_role(session_user, 'control_plane_runtime', 'member'),
    identity.non_superuser,
    identity.no_bypass_rls,
    identity.principal_status,
    identity.principal_generation,
    identity.login_enabled
FROM control_plane.runtime_identity() AS identity
