-- name: ssh_execution_test__publication :one
SELECT invocation.state,invocation.result_summary,
 (SELECT count(*) FROM control_plane.integration_effect_receipts receipt
  WHERE receipt.invocation_id=invocation.id AND receipt.organization_id=invocation.organization_id)
FROM control_plane.integration_invocations invocation
WHERE invocation.organization_id=$1::uuid AND invocation.ref=$2;
