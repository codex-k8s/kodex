INSERT INTO control_plane.schedule_occurrence_capabilities (
    id, organization_id, project_id, occurrence_id, attempt,
    immutable_input_sha256, authority_generation, full_method,
    workload_id, caller_spiffe_id, token_sha256, state, issued_at, expires_at
) VALUES (
    @id::uuid, @organization_id::uuid, @project_id::uuid, @occurrence_id::uuid, @attempt,
    @immutable_input_sha256, @authority_generation, @full_method,
    @workload_id, @caller_spiffe_id, @token_sha256, @state, @issued_at, @expires_at
)
ON CONFLICT (occurrence_id, attempt, full_method, authority_generation) DO NOTHING
