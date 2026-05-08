-- name: runtime_artifact_ref__list :many
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
WHERE (@job_id::uuid IS NULL OR job_id = @job_id::uuid)
  AND (@slot_id::uuid IS NULL OR slot_id = @slot_id::uuid)
  AND (cardinality(@artifact_types::text[]) = 0 OR artifact_type = ANY(@artifact_types::text[]))
ORDER BY created_at DESC, id
LIMIT @limit::integer OFFSET @offset::integer;
