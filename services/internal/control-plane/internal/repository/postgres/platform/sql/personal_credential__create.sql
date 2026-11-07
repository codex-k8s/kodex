-- name: personal_credential__create :one
INSERT INTO control_plane.integration_personal_credentials(ref, organization_id, connection_id, subject_id)
SELECT @ref, connection.organization_id, connection.id, subject.id
FROM control_plane.integration_connections connection
JOIN control_plane.subjects subject ON subject.organization_id = connection.organization_id
 AND subject.id = @subject_id::uuid AND subject.kind = 'USER' AND subject.active
WHERE connection.organization_id = @organization_id::uuid AND connection.ref = @connection_ref
  AND connection.lifecycle_state = 'ACTIVE' AND connection.credential_policy = 'INITIATOR_REQUIRED'
  AND connection.version = @connection_version
RETURNING ref
