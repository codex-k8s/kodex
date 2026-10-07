-- name: managed_configuration_consumer_binding_exists :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.managed_configuration_bindings binding
    WHERE binding.organization_id = @organization_id::uuid
      AND binding.configuration_kind = @configuration_kind
      AND binding.consumer_kind = @consumer_kind
      AND binding.consumer_ref = @consumer_ref
);
