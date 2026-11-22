WITH stamp AS (SELECT clock_timestamp() AS at)
INSERT INTO control_plane.kubernetes_identity_upload_attempts(
 ref,organization_id,setup_id,generation,mode,workload_instance,fence_sha256,created_at,expires_at)
SELECT @ref,organization_id,id,@generation,@mode,'synthetic-broker',@fence_sha256,stamp.at,
 least(stamp.at+@duration_milliseconds::integer*interval '1 millisecond',setup.expires_at)
FROM control_plane.kubernetes_identity_setups setup CROSS JOIN stamp
WHERE setup.id=@setup_id::uuid
RETURNING id::text;
