-- name: kubernetes_execution__plan_insert :one
WITH observed AS MATERIALIZED(SELECT clock_timestamp() AS now)
INSERT INTO control_plane.kubernetes_execution_plans(ref,organization_id,invocation_id,binding_digest,snapshot_digest,proposal_digest,plan_digest,child_count,hard_deadline,created_at)
SELECT @plan_ref,@organization_id::uuid,@invocation_id::uuid,@binding_digest,@snapshot_digest,@proposal_digest,@plan_digest,@child_count,
 least(observed.now+make_interval(secs=>@timeout_seconds::integer),coalesce(@runtime_deadline::timestamptz,observed.now+interval '600 seconds')),observed.now
FROM observed
RETURNING id::text,hard_deadline;
