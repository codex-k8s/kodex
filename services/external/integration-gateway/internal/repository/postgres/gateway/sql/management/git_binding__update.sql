WITH changed AS (
    UPDATE integration_gateway.git_source_bindings AS binding
   SET version = @version, generation = @generation, status = @status, repository_key = @repository_key,
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
 WHERE binding.binding_id = @binding_id AND binding.version = @expected_version
   AND NOT EXISTS (
       SELECT 1 FROM integration_gateway.management_effects AS effect
        WHERE effect.owner_kind = 'git_source_binding' AND effect.owner_id = binding.binding_id
          AND effect.status = 'CLAIMED' AND effect.dispatch_state = 'DISPATCHED'
   )
RETURNING binding.binding_id
), reconciliations_cancelled AS (
    UPDATE integration_gateway.git_reconciliations AS reconciliation
       SET state = 'CANCELLED', encrypted_snapshot = ''::bytea, updated_at = @updated_at
      FROM changed
     WHERE reconciliation.binding_id = changed.binding_id
       AND reconciliation.state IN ('PENDING', 'FETCHED')
    RETURNING reconciliation.reconciliation_id
), effects_cancelled AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = 'CANCELLED', dispatch_state = 'COMPLETED', lease_id = '',
           lease_expires_at = NULL, updated_at = @updated_at
      FROM changed
     WHERE effect.owner_kind = 'git_source_binding' AND effect.owner_id = changed.binding_id
       AND effect.status IN ('PENDING', 'CLAIMED') AND effect.dispatch_state = 'PENDING'
    RETURNING effect.effect_id
)
SELECT binding_id FROM changed
