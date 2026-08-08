INSERT INTO control_plane.legacy_graph_provenance (
    plan_id, ordinal, target_id, target_kind, source_table, source_ref,
    source_revision, source_sha256, immutable_input_sha256, root_actor_id, root_session_id,
    root_turn_id, root_attempt, runtime_revision_id, runtime_revision_version,
    parent_target_id, launching_turn_id, launching_attempt,
    launching_attempt_target_id, machine_policy_revision,
    machine_policy_sha256, legacy_policy_revision, legacy_policy_sha256,
    lineage_sha256
) VALUES (
    @plan_id::uuid, @ordinal, @target_id::uuid, @target_kind,
    @source_table, @source_ref, @source_revision, @source_sha256,
    nullif(@immutable_input_sha256, ''),
    @root_actor_id::uuid, nullif(@root_session_id, '')::uuid,
    nullif(@root_turn_id, '')::uuid, nullif(@root_attempt, 0),
    nullif(@runtime_revision_id, '')::uuid, nullif(@runtime_revision_version, 0),
    nullif(@parent_target_id, '')::uuid, nullif(@launching_turn_id, '')::uuid,
    nullif(@launching_attempt, 0), nullif(@launching_attempt_target_id, '')::uuid,
    nullif(@machine_policy_revision, 0), nullif(@machine_policy_sha256, ''),
    nullif(@legacy_policy_revision, 0), nullif(@legacy_policy_sha256, ''),
    @lineage_sha256
)
