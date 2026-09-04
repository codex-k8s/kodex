-- name: managed_configuration_list_bindings :many
SELECT binding.consumer_kind, binding.consumer_ref, revision.ref, binding.version
FROM control_plane.managed_configuration_bindings binding
JOIN control_plane.managed_configuration_sets configuration ON configuration.id = binding.configuration_set_id
JOIN control_plane.managed_configuration_revisions revision ON revision.id = binding.configuration_revision_id
WHERE configuration.organization_id = @organization_id::uuid AND configuration.ref = @configuration_ref
  AND binding.configuration_kind = configuration.kind
ORDER BY binding.consumer_kind, binding.consumer_ref;
