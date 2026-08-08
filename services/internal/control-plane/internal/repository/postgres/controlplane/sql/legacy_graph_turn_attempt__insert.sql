INSERT INTO control_plane.turn_attempts (
    turn_id, attempt, workload_id, authority_generation, state,
    input_sha256, lease_fence, started_at, finished_at, outcome,
    runtime_revision_id, runtime_revision_version
) VALUES (
    @turn_id::uuid, @attempt, @workload_id, @authority_generation,
    @state, @input_sha256, @lease_fence, @started_at,
    @finished_at, nullif(@outcome, ''), @runtime_revision_id::uuid,
    @runtime_revision_version
)
