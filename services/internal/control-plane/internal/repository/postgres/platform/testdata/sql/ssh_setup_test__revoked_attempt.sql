-- name: ssh_setup_test__revoked_attempt :one
SELECT setup.state,attempt.state,binding.state,
       binding.broker_identity_revision_id IS NULL
FROM control_plane.ssh_identity_setups setup
JOIN control_plane.ssh_identity_upload_attempts attempt ON attempt.setup_id=setup.id
 AND attempt.organization_id=setup.organization_id
JOIN control_plane.integration_broker_identity_revisions identity ON identity.id=setup.identity_revision_id
 AND identity.organization_id=setup.organization_id
JOIN control_plane.integration_personal_credentials binding ON binding.id=identity.binding_id
 AND binding.organization_id=setup.organization_id AND binding.connection_id=setup.connection_id
WHERE setup.ref=$1 AND attempt.ref=$2;
