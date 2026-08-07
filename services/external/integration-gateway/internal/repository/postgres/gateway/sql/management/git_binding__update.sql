UPDATE integration_gateway.git_source_bindings
   SET version = @version, status = @status, repository_key = @repository_key,
       ref_key = @ref_key, path_key = @path_key,
       repository_connection_id = @repository_connection_id,
       repository_connection_version = @repository_connection_version,
       repository_connection_sha256 = @repository_connection_sha256,
       credential_binding_id = @credential_binding_id,
       credential_binding_version = @credential_binding_version,
       credential_binding_sha256 = @credential_binding_sha256,
       target_kind = @target_kind, target_stable_key = @target_stable_key,
       fetched_commit = @fetched_commit, source_revision = @source_revision,
       source_sha256 = @source_sha256, fetched_at = @fetched_at,
       payload = @payload::jsonb, updated_at = @updated_at
 WHERE binding_id = @binding_id AND version = @expected_version
RETURNING binding_id
