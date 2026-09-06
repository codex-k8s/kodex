-- name: managed_configuration_cas_consumer :one
WITH inserted AS (
INSERT INTO control_plane.managed_configuration_bindings
    (ref, organization_id, project_id, configuration_set_id, configuration_revision_id,
     configuration_kind, consumer_kind, consumer_ref, rebound_by)
SELECT @binding_ref, @organization_id::uuid, @project_id::uuid, @configuration_set_id::uuid,
       @revision_id::uuid, @configuration_kind, @consumer_kind, @consumer_ref, @actor_id::uuid
WHERE @expected_absent::boolean
ON CONFLICT (organization_id, configuration_kind, consumer_kind, consumer_ref) DO NOTHING
RETURNING ref, consumer_kind, consumer_ref, configuration_revision_id, version
), updated AS (
UPDATE control_plane.managed_configuration_bindings
SET project_id = @project_id::uuid,
    configuration_set_id = @configuration_set_id::uuid,
    configuration_revision_id = @revision_id::uuid,
    version = managed_configuration_bindings.version + 1,
    rebound_by = @actor_id::uuid, updated_at = clock_timestamp()
WHERE NOT @expected_absent::boolean
  AND organization_id = @organization_id::uuid AND configuration_kind = @configuration_kind
  AND consumer_kind = @consumer_kind AND consumer_ref = @consumer_ref
  AND managed_configuration_bindings.version = @expected_version::bigint
  AND managed_configuration_bindings.configuration_revision_id = (
      SELECT id FROM control_plane.managed_configuration_revisions
      WHERE organization_id = @organization_id::uuid AND ref = @expected_revision_ref
  )
RETURNING ref, consumer_kind, consumer_ref, configuration_revision_id, version
), changed AS (SELECT * FROM inserted UNION ALL SELECT * FROM updated)
SELECT changed.ref, changed.consumer_kind, changed.consumer_ref, revision.ref, changed.version
FROM changed JOIN control_plane.managed_configuration_revisions revision ON revision.id = changed.configuration_revision_id;
