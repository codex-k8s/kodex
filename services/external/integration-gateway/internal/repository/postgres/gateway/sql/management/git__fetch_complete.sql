WITH binding_changed AS (
    UPDATE integration_gateway.git_source_bindings
       SET fetched_commit = @fetched_commit, source_revision = @source_revision,
           source_sha256 = @source_sha256, fetched_at = @fetched_at,
           payload = @binding_payload::jsonb, updated_at = @fetched_at
     WHERE binding_id = @binding_id AND version = @binding_version
       AND generation = @binding_generation AND status = 'ACTIVE'
       AND source_revision < @source_revision
    RETURNING binding_id, version, generation, status
), reconciliation_changed AS (
    UPDATE integration_gateway.git_reconciliations AS reconciliation
       SET state = 'FETCHED', fetched_commit = @fetched_commit, source_revision = @source_revision,
           source_sha256 = @source_sha256, encrypted_snapshot = @encrypted_snapshot,
           command_intent_sha256 = @command_intent_sha256,
           receipt_sha256 = @receipt_sha256, updated_at = @fetched_at
      FROM binding_changed
     WHERE reconciliation.reconciliation_id = @reconciliation_id
       AND reconciliation.state = 'PENDING'
    RETURNING reconciliation.reconciliation_id
), fetch_completed AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = 'SUCCEEDED', dispatch_state = 'COMPLETED', lease_id = '', lease_expires_at = NULL, updated_at = @fetched_at
      FROM reconciliation_changed
     WHERE effect.effect_id = @effect_id AND effect.status = 'CLAIMED'
       AND effect.lease_id = @lease_id AND effect.lease_fence = @lease_fence
       AND effect.dispatch_state = 'DISPATCHED'
       AND effect.owner_kind = 'git_source_binding'
       AND effect.owner_id = @binding_id AND effect.owner_version = @binding_version
       AND effect.owner_generation = @binding_generation AND effect.owner_status = 'ACTIVE'
       AND effect.input_sha256 = @command_intent_sha256
    RETURNING effect_id
)
INSERT INTO integration_gateway.management_effects (
    effect_id, tenant_id, project_id, actor_id, effect_kind, resource_kind, resource_id,
    resource_version, resource_generation, intent_sha256, owner_kind, owner_id,
    owner_version, owner_generation, owner_status, input_sha256, status, available_at, payload, created_at, updated_at
)
SELECT @apply_effect_id, @tenant_id, @project_id, @actor_id, 'GIT_APPLY', 'git_reconciliation',
       reconciliation_id, @binding_version, (SELECT generation FROM binding_changed), @intent_sha256,
       'git_source_binding', (SELECT binding_id FROM binding_changed), (SELECT version FROM binding_changed),
       (SELECT generation FROM binding_changed), (SELECT status FROM binding_changed), @intent_sha256, 'PENDING', @fetched_at,
       @effect_payload::jsonb, @fetched_at, @fetched_at
  FROM reconciliation_changed WHERE EXISTS (SELECT 1 FROM fetch_completed)
RETURNING effect_id
