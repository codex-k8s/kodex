SELECT
    state.version,
    pg_has_role(current_user, 'control_plane_runtime', 'member'),
    NOT role.rolsuper,
    NOT role.rolbypassrls
FROM control_plane.schema_state AS state
JOIN pg_catalog.pg_roles AS role ON role.rolname = current_user
WHERE state.singleton = true
