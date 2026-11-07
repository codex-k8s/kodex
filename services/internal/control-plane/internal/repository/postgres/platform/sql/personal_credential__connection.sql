-- name: personal_credential__connection :one
SELECT connection.id::text, connection.credential_policy, connection.version
FROM control_plane.integration_connections connection
JOIN control_plane.subjects subject ON subject.organization_id = connection.organization_id
 AND subject.id = @subject_id::uuid AND subject.kind = 'USER' AND subject.active
WHERE connection.organization_id = @organization_id::uuid AND connection.ref = @connection_ref
  AND connection.lifecycle_state = 'ACTIVE'
FOR UPDATE OF connection
