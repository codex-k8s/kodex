-- name: services_policy__get_active :one
SELECT
    id, project_id, source_repository_id, source_path, source_ref,
    source_commit_sha, source_blob_sha, policy_version, content_hash,
    validated_payload, validation_status, projection_status, imported_at,
    version, created_at, updated_at
FROM project_catalog_services_policies
WHERE project_id = @project_id
  AND validation_status = 'valid'
  AND projection_status IN ('synced', 'overridden')
ORDER BY policy_version DESC, imported_at DESC, id
LIMIT 1;
