-- name: kubernetes_setup__attempt_complete :exec
UPDATE control_plane.kubernetes_identity_upload_attempts SET state=@state,closed_at=clock_timestamp()
WHERE id=@attempt_id::uuid AND organization_id=@organization_id::uuid AND setup_id=@setup_id::uuid AND state='ACTIVE';
