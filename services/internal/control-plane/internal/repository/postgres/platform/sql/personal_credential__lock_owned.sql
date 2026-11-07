-- name: personal_credential__lock_owned :one
SELECT binding.id::text, connection.credential_policy, connection.version
FROM control_plane.integration_personal_credentials binding
JOIN control_plane.integration_connections connection
  ON connection.id = binding.connection_id AND connection.organization_id = binding.organization_id
JOIN control_plane.subjects subject
  ON subject.id = binding.subject_id AND subject.organization_id = binding.organization_id
 AND subject.kind = 'USER' AND subject.active
WHERE binding.organization_id = @organization_id::uuid
  AND binding.subject_id = @subject_id::uuid
  AND connection.ref = @connection_ref
  AND connection.lifecycle_state = 'ACTIVE'
FOR UPDATE OF connection, binding
