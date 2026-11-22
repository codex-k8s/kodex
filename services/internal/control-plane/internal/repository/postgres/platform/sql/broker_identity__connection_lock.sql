-- name: broker_identity__connection_lock :one
SELECT connection.version,connection.credential_policy
FROM control_plane.integration_connections connection
JOIN control_plane.subjects subject ON subject.organization_id=connection.organization_id
 AND subject.id=@subject_id::uuid AND subject.active
 AND ((connection.credential_policy='INITIATOR_REQUIRED' AND subject.kind='USER')
  OR (connection.credential_policy='PROJECT_SHARED' AND subject.kind='SERVICE'))
WHERE connection.organization_id=@organization_id::uuid AND connection.id=@connection_id::uuid
 AND connection.ref=@connection_ref AND connection.lifecycle_state='ACTIVE'
FOR UPDATE OF connection FOR SHARE OF subject;
