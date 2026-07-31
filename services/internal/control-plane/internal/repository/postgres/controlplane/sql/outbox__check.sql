SELECT
    state.version,
    pg_has_role(current_user, 'control_plane_relay', 'member'),
    NOT role.rolsuper,
    NOT role.rolbypassrls,
    (SELECT count(*) FROM control_plane.outbox_events
      WHERE terminal = true AND published_at IS NULL)
FROM control_plane.schema_state AS state
JOIN pg_catalog.pg_roles AS role ON role.rolname = current_user
WHERE state.singleton = true
