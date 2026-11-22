-- name: artifacts_purge_manual_issue :one
SELECT retention_claim_generation+1,
 least(clock_timestamp()+interval '30 seconds',coalesce(@access_until::timestamptz,'infinity'::timestamptz))
FROM control_plane.artifact_heads
WHERE id=@artifact_id::uuid AND organization_id=@organization_id::uuid
 AND lifecycle_state='DELETED' AND version=@expected_version
 AND retention_claim_generation<9223372036854775807
FOR UPDATE;
