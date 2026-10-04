-- name: assistant_configuration_component_account_binding_constraint :one
SELECT convalidated, pg_get_constraintdef(oid)
FROM pg_constraint
WHERE conrelid = 'control_plane.access_bindings'::regclass
  AND conname = 'access_bindings_scope_shape';
