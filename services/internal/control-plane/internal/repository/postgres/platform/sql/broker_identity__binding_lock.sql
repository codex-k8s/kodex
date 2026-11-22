-- name: broker_identity__binding_lock :one
SELECT ref,version,generation FROM control_plane.integration_personal_credentials
WHERE organization_id=@organization_id::uuid AND id=@binding_id::uuid
 AND connection_id=@connection_id::uuid AND subject_id=@subject_id::uuid
 AND credential_kind='BROKER_IDENTITY' AND broker_purpose=@purpose
FOR UPDATE;
