SELECT
    id::text,
    organization_id::text,
    project_id::text,
    actor_id::text,
    action,
    resource_id::text,
    resource_kind,
    resource_version,
    outcome,
    correlation_id::text,
    policy_revision,
    occurred_at
FROM control_plane.audit_events
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND resource_id = ANY(@resource_ids::uuid[])
  AND (
      NOT @has_after::boolean
      OR occurred_at > @after_occurred_at::timestamptz
      OR (
          occurred_at = @after_occurred_at::timestamptz
          AND id > nullif(@after_id, '')::uuid
      )
  )
ORDER BY occurred_at, id
LIMIT @limit;
