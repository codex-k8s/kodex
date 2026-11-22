-- name: ssh_setup__recovery_eligibility :one
SELECT EXISTS(SELECT 1 FROM control_plane.ssh_identity_upload_attempts a WHERE a.setup_id=@setup_id::uuid AND a.mode='WRITE'),
 EXISTS(SELECT 1 FROM control_plane.ssh_identity_upload_attempts a
  JOIN control_plane.ssh_identity_upload_observations o ON o.attempt_id=a.id AND o.organization_id=a.organization_id
  WHERE a.setup_id=@setup_id::uuid AND a.mode='WRITE' AND o.outcome='UNKNOWN' AND o.recorded_at<a.expires_at)
 AND NOT EXISTS(SELECT 1 FROM control_plane.ssh_identity_upload_attempts attempted
  WHERE attempted.setup_id=@setup_id::uuid AND NOT EXISTS(
   SELECT 1 FROM control_plane.ssh_identity_upload_observations reported
   WHERE reported.attempt_id=attempted.id AND reported.organization_id=attempted.organization_id
    AND reported.outcome='UNKNOWN' AND reported.recorded_at<attempted.expires_at)),
 EXISTS(SELECT 1 FROM control_plane.ssh_identity_ack_rejections r WHERE r.setup_id=@setup_id::uuid),
 EXISTS(SELECT 1 FROM control_plane.ssh_identity_material_revisions m WHERE m.identity_revision_id=@identity_id::uuid),
 EXISTS(SELECT 1 FROM control_plane.ssh_identity_upload_attempts a WHERE a.setup_id=@setup_id::uuid AND a.state='ACTIVE' AND a.expires_at>clock_timestamp()),
 coalesce((SELECT max(a.generation) FROM control_plane.ssh_identity_upload_attempts a WHERE a.setup_id=@setup_id::uuid),0);
