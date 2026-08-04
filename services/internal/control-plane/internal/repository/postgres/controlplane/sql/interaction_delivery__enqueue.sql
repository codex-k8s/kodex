INSERT INTO control_plane.interaction_delivery_work (
    id, organization_id, project_id, actor_id, session_id, session_version,
    turn_id, turn_version, attempt, runtime_revision_id, runtime_revision_version,
    immutable_input_sha256, kind, lifecycle_state, outcome,
    artifact_id, artifact_version, artifact_sha256, artifact_name, artifact_storage_ref,
    artifact_size_bytes, artifact_media_type, inline_payload
) VALUES (@id, @organization_id, @project_id, @actor_id, @session_id, @session_version,
    @turn_id, @turn_version, @attempt, @runtime_revision_id, @runtime_revision_version,
    @immutable_input_sha256, @kind, @lifecycle_state, @outcome,
    NULLIF(@artifact_id, '')::uuid, NULLIF(@artifact_version, 0), @artifact_sha256,
    @artifact_name, @artifact_storage_ref, @artifact_size_bytes, @artifact_media_type, @inline_payload)
ON CONFLICT (id) DO NOTHING;
