-- name: ssh_health__material :one
SELECT i.id::text,m.namespace,m.secret_name,m.secret_uid,m.secret_resource_version,m.content_sha256,m.authentication_sha256
FROM control_plane.integration_broker_identity_revisions i
JOIN control_plane.integration_connections c ON c.id=i.connection_id AND c.organization_id=i.organization_id
JOIN control_plane.ssh_identity_material_revisions m ON m.identity_revision_id=i.id AND m.organization_id=i.organization_id AND m.connection_id=i.connection_id
JOIN control_plane.ssh_identity_upload_observations o ON o.attempt_id=m.attempt_id AND o.organization_id=i.organization_id AND o.outcome='KNOWN'
WHERE i.organization_id=@organization_id::uuid AND c.ref=@connection_ref AND i.ref=@identity_ref AND i.revision=@revision AND i.target_ref=@target_ref AND i.purpose='SSH_TARGET'
 AND NOT EXISTS(SELECT 1 FROM control_plane.ssh_identity_ack_rejections r WHERE r.identity_revision_id=i.id)
 AND NOT EXISTS(SELECT 1 FROM control_plane.ssh_identity_quarantines q WHERE q.identity_revision_id=i.id);
