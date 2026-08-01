SELECT
    organization_id::text,
    coalesce(project_id::text, ''),
    scope,
    key_hash,
    request_hash,
    result,
    payload,
    created_at
FROM control_plane.command_receipts
WHERE organization_id = @organization_id::uuid
  AND scope = @scope
  AND key_hash = @key_hash
