INSERT INTO integration_gateway.git_source_bindings (
    binding_id, tenant_id, project_id, stable_key, version, generation, status,
    repository_key, ref_key, path_key, repository_connection_id,
    repository_connection_version, repository_connection_sha256,
    credential_binding_id, credential_binding_version, credential_binding_sha256,
    target_kind, target_stable_key, payload, created_at, updated_at
) VALUES (
    @binding_id, @tenant_id, @project_id, @stable_key, @version, @generation, @status,
    @repository_key, @ref_key, @path_key, @repository_connection_id,
    @repository_connection_version, @repository_connection_sha256,
    @credential_binding_id, @credential_binding_version, @credential_binding_sha256,
    @target_kind, @target_stable_key, @payload::jsonb, @created_at, @updated_at
)
