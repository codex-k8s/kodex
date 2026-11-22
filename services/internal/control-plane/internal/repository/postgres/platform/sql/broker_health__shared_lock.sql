-- name: broker_health__shared_lock :one
SELECT t.id::text,t.connection_id::text,t.state,COALESCE(t.lease_ref,''),COALESCE(t.fence_digest,''),COALESCE(t.workload_instance,''),t.generation,t.lease_expires_at,t.input_digest,
 t.credential_kind,COALESCE(t.broker_purpose,''),COALESCE(t.broker_identity_revision_id::text,''),t.input_snapshot->>'brokerIdentityRef',COALESCE((t.input_snapshot->>'brokerIdentityRevision')::bigint,0),t.input_snapshot->>'brokerIdentityTargetRef'
FROM control_plane.integration_connection_tests t WHERE t.organization_id=@organization_id::uuid AND t.ref=@ref FOR UPDATE;
