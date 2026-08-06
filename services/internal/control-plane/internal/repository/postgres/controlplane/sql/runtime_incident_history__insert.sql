INSERT INTO control_plane.runtime_incident_history (
    organization_id, project_id, incident_id, version, owner_actor_id,
    state, action, reason_code, occurred_at
) VALUES (
    @organization_id::uuid, @project_id::uuid, @incident_id::uuid,
    @version, @owner_actor_id::uuid, @state, @action, @reason_code,
    @occurred_at
);
