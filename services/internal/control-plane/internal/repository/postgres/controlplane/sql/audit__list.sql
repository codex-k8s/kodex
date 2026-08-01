-- name: AuditList
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
  AND (@resource_kind = '' OR resource_kind = @resource_kind)
  AND (@resource_id = '' OR resource_id = @resource_id::uuid)
  AND (@action = '' OR action = @action)
  AND id > coalesce(
      nullif(@after_id, '')::uuid,
      '00000000-0000-0000-0000-000000000000'::uuid
  )
ORDER BY id
LIMIT @limit
