INSERT INTO control_plane.resources (
    id,
    organization_id,
    project_id,
    parent_id,
    owner_actor_id,
    kind,
    name,
    state,
    version,
    spec,
    schedule_next_run_at,
    created_at,
    updated_at
) VALUES (
    @id::uuid,
    @organization_id::uuid,
    @project_id::uuid,
    nullif(@parent_id, '')::uuid,
    @owner_actor_id::uuid,
    @kind,
    @name,
    @state,
    @version,
    @spec::jsonb,
    @schedule_next_run_at,
    @created_at,
    @updated_at
)
