-- name: services_policy__insert :exec
INSERT INTO project_catalog_services_policies (
    id, project_id, source_repository_id, source_path, source_ref,
    source_commit_sha, source_blob_sha, policy_version, content_hash,
    validated_payload, validation_status, projection_status, imported_at,
    version, created_at, updated_at
) VALUES (
    @id, @project_id, @source_repository_id, @source_path, @source_ref,
    @source_commit_sha, @source_blob_sha, @policy_version, @content_hash,
    @validated_payload, @validation_status, @projection_status, @imported_at,
    @version, @created_at, @updated_at
);
