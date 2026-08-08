WITH winner AS (
    SELECT reconciliation.reconciliation_id
      FROM integration_gateway.git_reconciliations AS reconciliation
      JOIN integration_gateway.git_source_bindings AS binding
        ON binding.binding_id = reconciliation.binding_id
      JOIN integration_gateway.management_effects AS effect
        ON effect.effect_id = @effect_id
     WHERE reconciliation.reconciliation_id = @reconciliation_id
       AND reconciliation.binding_id = @binding_id
       AND reconciliation.binding_version = @binding_version
       AND reconciliation.source_revision = @source_revision
       AND reconciliation.source_sha256 = @source_sha256
       AND reconciliation.state = 'FETCHED'
       AND binding.binding_id = @binding_id AND binding.version = @binding_version
       AND binding.generation = @binding_generation AND binding.status = 'ACTIVE'
       AND binding.source_revision = @source_revision AND binding.source_sha256 = @source_sha256
       AND effect.effect_kind = 'GIT_APPLY' AND effect.resource_id = @reconciliation_id
       AND effect.status = 'CLAIMED' AND effect.lease_id = @lease_id
       AND effect.lease_fence = @lease_fence AND effect.dispatch_state = 'DISPATCHED'
       AND effect.owner_kind = 'git_source_binding' AND effect.owner_id = @binding_id
       AND effect.owner_version = @binding_version AND effect.owner_generation = @binding_generation
       AND effect.owner_status = 'ACTIVE' AND effect.input_sha256 = reconciliation.command_intent_sha256
     FOR UPDATE OF reconciliation, binding, effect
), changed AS (
    UPDATE integration_gateway.git_reconciliations AS reconciliation
       SET state = 'APPLIED', encrypted_snapshot = ''::bytea,
           target_resource_id = @target_resource_id, target_version = @target_version,
           target_sha256 = @target_sha256, updated_at = @updated_at
      FROM winner
     WHERE reconciliation.reconciliation_id = winner.reconciliation_id
    RETURNING reconciliation.reconciliation_id
), completed AS (
    UPDATE integration_gateway.management_effects AS effect
       SET status = 'SUCCEEDED', dispatch_state = 'COMPLETED', lease_id = '', lease_expires_at = NULL, updated_at = @updated_at
      FROM changed
     WHERE effect.effect_id = @effect_id AND effect.status = 'CLAIMED'
       AND effect.lease_id = @lease_id AND effect.lease_fence = @lease_fence
       AND effect.dispatch_state = 'DISPATCHED'
    RETURNING effect_id
)
SELECT reconciliation_id FROM changed WHERE EXISTS (SELECT 1 FROM completed)
