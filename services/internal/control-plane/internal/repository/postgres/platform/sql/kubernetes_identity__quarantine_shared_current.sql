-- name: kubernetes_identity__quarantine_shared_current :one
SELECT EXISTS(SELECT 1 FROM control_plane.integration_connections
WHERE organization_id=@organization_id::uuid AND id=@connection_id::uuid
 AND broker_identity_revision_id=@identity_id::uuid AND credential_policy='PROJECT_SHARED');
