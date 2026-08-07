WITH inserted AS (
    INSERT INTO control_plane.runtime_derived_resources (
        id, organization_id, project_id, parent_id, owner_actor_id, kind,
        name, state, version, spec, source_kind, source_id, source_version,
        source_sha256, created_at, updated_at
    ) VALUES (
        @id::uuid, @organization_id::uuid, @project_id::uuid,
        nullif(@parent_id, '')::uuid, @owner_actor_id::uuid, @kind,
        @name, 'ACTIVE', @version, @spec::jsonb, @source_kind,
        @source_id::uuid, @source_version, @source_sha256,
        @created_at, @updated_at
    )
    ON CONFLICT DO NOTHING
    RETURNING 1
)
SELECT EXISTS (SELECT 1 FROM inserted) OR EXISTS (
    SELECT 1
    FROM control_plane.runtime_derived_resources
    WHERE organization_id = @organization_id::uuid
      AND project_id = @project_id::uuid
      AND id = @id::uuid
      AND version = @version
      AND kind = @kind
      AND parent_id IS NOT DISTINCT FROM nullif(@parent_id, '')::uuid
      AND owner_actor_id = @owner_actor_id::uuid
      AND name = @name
      AND state = 'ACTIVE'
      AND source_kind = @source_kind
      AND source_id = @source_id::uuid
      AND source_version = @source_version
      AND source_sha256 = @source_sha256
      AND spec = @spec::jsonb
      AND created_at = @created_at::timestamptz
      AND updated_at = @updated_at::timestamptz
)
