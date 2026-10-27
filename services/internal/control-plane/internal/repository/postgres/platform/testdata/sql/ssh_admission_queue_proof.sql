-- name: ssh_admission_queue_proof :one
SELECT invocation.state,invocation.safe_error_code,COALESCE(receipt.ref,''),
 invocation.ssh_compose_snapshot,invocation.generation,invocation.ssh_compose_admission_blocked,
 invocation.ssh_compose_admission_block_reason,
 (SELECT count(*) FROM control_plane.audit_events audit WHERE audit.organization_id=invocation.organization_id
  AND audit.resource_ref=invocation.ref AND audit.action='integration.ssh-compose.admission.block'),
 (SELECT count(*) FROM control_plane.run_events event WHERE event.organization_id=invocation.organization_id
  AND event.aggregate_ref=invocation.ref AND event.type='TURN_PROGRESS'
  AND event.safe_summary='i18n:INTEGRATION_ADMISSION_BLOCKED')
FROM control_plane.integration_invocations invocation
LEFT JOIN control_plane.integration_effect_receipts receipt ON receipt.id=invocation.effect_receipt_id
WHERE invocation.ref=$1;
