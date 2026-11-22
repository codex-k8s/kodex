-- name: kubernetes_identity__quarantine_personal_lock :one
SELECT ref FROM control_plane.integration_personal_credentials
WHERE organization_id=@organization_id::uuid AND id=@binding_id::uuid AND connection_id=@connection_id::uuid
 AND subject_id=@subject_id::uuid AND credential_kind='BROKER_IDENTITY' AND broker_purpose='KUBERNETES_TARGET'
 AND broker_identity_revision_id=@identity_id::uuid AND state<>'REVOKED'
FOR UPDATE;
