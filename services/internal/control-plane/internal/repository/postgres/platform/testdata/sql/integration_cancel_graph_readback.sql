-- name: integration_cancel_graph_readback :one
SELECT invocation.state,
       invocation.lease_ref IS NULL AND invocation.effect_fence_digest IS NULL
         AND invocation.workload_instance IS NULL AND invocation.lease_expires_at IS NULL,
       run.state,
       (SELECT count(*) FROM control_plane.runtime_leases lease
        JOIN control_plane.runs child ON child.id=lease.run_id
        WHERE child.root_run_id=run.root_run_id AND lease.state='CLAIMED'),
       (SELECT count(*) FROM control_plane.integration_approval_scopes approval
        WHERE approval.root_run_id=run.root_run_id AND approval.revoked_at IS NULL),
       (SELECT count(*) FROM control_plane.run_events event
        WHERE event.root_run_id=run.root_run_id AND event.aggregate_ref=invocation.ref
          AND event.type='TURN_PROGRESS' AND event.run_state='CANCELLED'),
       (SELECT count(*) FROM control_plane.idempotency_receipts WHERE organization_id=run.organization_id),
       (SELECT count(*) FROM control_plane.audit_events WHERE organization_id=run.organization_id),
       (SELECT count(*) FROM control_plane.outbox_events)
FROM control_plane.integration_invocations invocation
JOIN control_plane.runs run ON run.id=invocation.run_id
WHERE invocation.ref=$1 AND run.ref=$2
