-- name: runtime_artifact_ref__get :one
SELECT
    id,
    job_id,
    slot_id,
    artifact_type,
    external_ref,
    digest,
    metadata_json,
    created_at
FROM runtime_manager_artifact_refs
WHERE id = @id;
