INSERT INTO control_plane.cache_epochs (
    organization_id,
    scope_key,
    project_id,
    epoch,
    updated_at
) VALUES (
    @organization_id::uuid,
    CASE WHEN @project_id = '' THEN 'tenant' ELSE @project_id END,
    nullif(@project_id, '')::uuid,
    1,
    clock_timestamp()
)
ON CONFLICT (organization_id, scope_key) DO UPDATE
SET
    epoch = control_plane.cache_epochs.epoch + 1,
    updated_at = excluded.updated_at
RETURNING epoch
