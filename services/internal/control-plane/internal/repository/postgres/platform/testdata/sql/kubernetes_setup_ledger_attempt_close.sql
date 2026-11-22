UPDATE control_plane.kubernetes_identity_upload_attempts
SET state=@state,closed_at=clock_timestamp() WHERE setup_id=@setup_id::uuid AND state='ACTIVE';
