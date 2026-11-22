DELETE FROM control_plane.kubernetes_identity_upload_observations WHERE attempt_id=@attempt_id::uuid;
