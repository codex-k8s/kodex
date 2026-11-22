-- name: kubernetes_tool__stop_test_evidence :one
SELECT
 (SELECT count(*) FROM control_plane.audit_events audit WHERE audit.organization_id=invocation.organization_id
  AND audit.resource_ref=invocation.ref AND audit.action='platform.integrations.kubernetes.tool.stop'),
 (SELECT count(*) FROM control_plane.idempotency_receipts receipt WHERE receipt.organization_id=invocation.organization_id
  AND receipt.actor_id=@actor_id::uuid AND receipt.operation='platform.integrations.kubernetes.tool.stop'
  AND receipt.idempotency_key='tool-stop-once'),
 invocation.lease_ref IS NULL AND invocation.effect_fence_digest IS NULL AND invocation.workload_instance IS NULL
FROM control_plane.integration_invocations invocation
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref;
