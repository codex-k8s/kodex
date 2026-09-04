-- name: managed_configuration_rebind_consumer :one
INSERT INTO control_plane.managed_configuration_bindings
    (ref, organization_id, project_id, configuration_set_id, configuration_revision_id,
     configuration_kind, consumer_kind, consumer_ref, rebound_by)
VALUES (@binding_ref, @organization_id::uuid, @project_id::uuid, @configuration_set_id::uuid,
        @revision_id::uuid, @configuration_kind, @consumer_kind, @consumer_ref, @actor_id::uuid)
ON CONFLICT (organization_id, configuration_kind, consumer_kind, consumer_ref) DO UPDATE
SET project_id = EXCLUDED.project_id,
    configuration_set_id = EXCLUDED.configuration_set_id,
    configuration_revision_id = EXCLUDED.configuration_revision_id,
    version = managed_configuration_bindings.version + 1,
    rebound_by = EXCLUDED.rebound_by, updated_at = clock_timestamp()
RETURNING ref, consumer_kind, consumer_ref,
          (SELECT ref FROM control_plane.managed_configuration_revisions WHERE id = configuration_revision_id), version;
