-- name: runtime_artifact_ref__insert :exec
INSERT INTO runtime_manager_artifact_refs (
    id,
    job_id,
    slot_id,
    artifact_type,
    external_ref,
    digest,
    metadata_json,
    created_at
) VALUES (
    @id,
    @job_id::uuid,
    @slot_id::uuid,
    @artifact_type,
    @external_ref,
    @digest,
    @metadata_json::jsonb,
    @created_at
);
