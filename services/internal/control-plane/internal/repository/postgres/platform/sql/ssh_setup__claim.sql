-- name: ssh_setup__claim :one
WITH observed AS MATERIALIZED (SELECT clock_timestamp() AS now), closed AS (
 UPDATE control_plane.ssh_identity_upload_attempts SET state='CLOSED',closed_at=clock_timestamp(),close_reason='LEASE_EXPIRED'
 WHERE setup_id=@setup_id::uuid AND state='ACTIVE' AND expires_at<=clock_timestamp() RETURNING id
), updated AS (
 UPDATE control_plane.ssh_identity_setups
 SET authentication_sha256=@authentication_digest,state='ATTEMPTING',reason_code='ATTEMPTING',version=version+1
 WHERE id=@setup_id::uuid AND organization_id=@organization_id::uuid AND version=@version
  AND state IN('PREPARED','ATTEMPTING','UNKNOWN') AND expires_at>clock_timestamp()
  AND (authentication_sha256 IS NULL OR authentication_sha256=@authentication_digest)
  AND (SELECT count(*) FROM closed)>=0
 RETURNING id,organization_id,expires_at
)
INSERT INTO control_plane.ssh_identity_upload_attempts(ref,organization_id,setup_id,generation,mode,workload_instance,fence_sha256,snapshot_sha256,state,created_at,expires_at)
SELECT @attempt_ref,updated.organization_id,updated.id,@generation,@mode,@instance,@fence_digest,@snapshot_digest,'ACTIVE',observed.now,least(updated.expires_at,observed.now+interval '30 seconds',@access_until::timestamptz)
FROM updated CROSS JOIN observed
WHERE least(updated.expires_at,observed.now+interval '30 seconds',@access_until::timestamptz)>clock_timestamp()
RETURNING expires_at,created_at;
