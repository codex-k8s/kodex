-- name: ssh_setup__capacity :one
SELECT count(*)<64 FROM control_plane.ssh_identity_setups setup
WHERE setup.organization_id=@organization_id::uuid AND setup.actor_id=@actor_id::uuid
 AND (setup.state IN('PREPARED','ATTEMPTING','UNKNOWN') OR
  (EXISTS(SELECT 1 FROM control_plane.ssh_identity_upload_attempts attempt WHERE attempt.setup_id=setup.id AND attempt.mode='WRITE')
   AND NOT EXISTS(SELECT 1 FROM control_plane.ssh_identity_material_revisions material WHERE material.identity_revision_id=setup.identity_revision_id)));
