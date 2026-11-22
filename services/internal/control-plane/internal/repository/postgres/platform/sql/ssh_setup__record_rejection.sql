-- name: ssh_setup__record_rejection :exec
INSERT INTO control_plane.ssh_identity_ack_rejections(organization_id,setup_id,attempt_id,identity_revision_id,response_sha256,truncated,observed_uid,recorded_at)
VALUES(@organization_id::uuid,@setup_id::uuid,@attempt_id::uuid,@identity_id::uuid,@response_digest,@truncated,@uid,clock_timestamp())
ON CONFLICT(attempt_id,response_sha256,truncated,observed_uid) DO NOTHING;
