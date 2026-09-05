-- name: secret_draft_operation_lock :one
SELECT o.id::text,o.ref,o.kind,o.state,o.actor_id::text,o.expected_draft_version,
o.expected_secret_version,o.expected_current_revision,o.target_revision,o.intent_digest,
o.claimant_id,o.claim_generation,o.grant_expires_at,o.lease_deadline,o.failure_code,o.terminal_snapshot,
o.encrypted_cleanup_descriptor,o.materialization_cleanup_descriptor,o.cleanup_completed,o.correlation_ref
FROM control_plane.runtime_secret_draft_operations o
WHERE o.organization_id=@organization_id::uuid AND o.ref=@operation_ref
FOR UPDATE;
