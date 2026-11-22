-- name: broker_identity__ssh_fact :one
SELECT max(least(setup.expires_at,attempt.expires_at))
FROM control_plane.ssh_identity_setups setup
JOIN control_plane.ssh_identity_material_revisions material ON material.setup_id=setup.id
 AND material.organization_id=setup.organization_id AND material.identity_revision_id=setup.identity_revision_id
 AND material.content_sha256=setup.content_sha256 AND material.authentication_sha256=setup.authentication_sha256
 AND material.namespace=setup.material_namespace
JOIN control_plane.ssh_identity_upload_attempts attempt ON attempt.id=material.attempt_id
 AND attempt.organization_id=setup.organization_id AND attempt.setup_id=setup.id
 AND attempt.state='CLOSED' AND attempt.close_reason='COMPLETED' AND attempt.snapshot_sha256=setup.snapshot_sha256
JOIN control_plane.ssh_identity_upload_observations observation ON observation.attempt_id=attempt.id
 AND observation.organization_id=setup.organization_id AND observation.setup_id=setup.id AND observation.outcome='KNOWN'
WHERE setup.identity_revision_id=@identity_id::uuid AND setup.organization_id=@organization_id::uuid
 AND setup.connection_id=@connection_id::uuid AND setup.state='COMPLETED'
 AND setup.expires_at>clock_timestamp() AND attempt.expires_at>clock_timestamp()
 AND setup.material_namespace='kodex-ssh-identities'
 AND NOT EXISTS(SELECT 1 FROM control_plane.ssh_identity_ack_rejections rejection WHERE rejection.setup_id=setup.id)
 AND NOT EXISTS(SELECT 1 FROM control_plane.ssh_identity_quarantines quarantine WHERE quarantine.identity_revision_id=setup.identity_revision_id);
