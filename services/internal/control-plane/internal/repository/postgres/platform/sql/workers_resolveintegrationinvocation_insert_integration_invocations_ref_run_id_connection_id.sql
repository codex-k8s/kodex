-- name: workers_resolveintegrationinvocation_insert_integration_invocations_ref_run_id_connection_id :one
INSERT INTO control_plane.integration_invocations(
	ref,organization_id,run_id,node_id,connection_id,grant_id,capability_key,operation,idempotency_key,
	intent_digest,input_digest,bounded_input,state,definition_version,definition_digest,risk,
	approval_policy,resource_kind,resource_scope,resource_scope_digest,effect_key,mailbox_gate_required
)
VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
ON CONFLICT(node_id,idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key
WHERE control_plane.integration_invocations.intent_digest=EXCLUDED.intent_digest
RETURNING id::text,ref,state
