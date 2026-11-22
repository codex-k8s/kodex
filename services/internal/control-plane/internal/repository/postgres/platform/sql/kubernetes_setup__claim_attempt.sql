-- name: kubernetes_setup__claim_attempt :one
WITH stamp AS (SELECT clock_timestamp() AS at), next_generation AS (
 SELECT coalesce(max(generation),0)+1 AS value FROM control_plane.kubernetes_identity_upload_attempts
 WHERE setup_id=@setup_id::uuid AND organization_id=@organization_id::uuid
)
INSERT INTO control_plane.kubernetes_identity_upload_attempts(
 ref,organization_id,setup_id,generation,mode,workload_instance,fence_sha256,created_at,expires_at)
SELECT @ref,setup.organization_id,setup.id,next_generation.value,@mode,@instance,@fence_digest,stamp.at,
 least(stamp.at+interval '30 seconds',setup.expires_at,@access_until::timestamptz)
FROM control_plane.kubernetes_identity_setups setup CROSS JOIN stamp CROSS JOIN next_generation
WHERE setup.id=@setup_id::uuid AND setup.organization_id=@organization_id::uuid
 AND setup.state IN('PREPARED','ATTEMPTING','UNKNOWN') AND setup.expires_at>stamp.at
 AND least(stamp.at+interval '30 seconds',setup.expires_at,@access_until::timestamptz)>clock_timestamp()
 AND next_generation.value<=8
RETURNING ref,generation,expires_at,created_at;
