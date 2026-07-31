INSERT INTO control_plane.schedule_occurrences (
    id,
    schedule_id,
    organization_id,
    project_id,
    scheduled_for,
    target_resource_id
) VALUES (
    @id::uuid,
    @schedule_id::uuid,
    @organization_id::uuid,
    @project_id::uuid,
    @scheduled_for,
    @target_resource_id::uuid
)
ON CONFLICT (schedule_id, scheduled_for) DO NOTHING
