INSERT INTO control_plane.command_receipts (
    organization_id,
    project_id,
    scope,
    key_hash,
    request_hash,
    result,
    payload,
    created_at
) VALUES (
    @organization_id::uuid,
    nullif(@project_id, '')::uuid,
    @scope,
    @key_hash,
    @request_hash,
    @result::jsonb,
    @payload::jsonb,
    @created_at
)
