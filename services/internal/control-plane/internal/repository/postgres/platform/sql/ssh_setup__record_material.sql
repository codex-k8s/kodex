-- name: ssh_setup__record_material :one
WITH inserted AS (
INSERT INTO control_plane.ssh_identity_material_revisions(organization_id,connection_id,identity_revision_id,setup_id,attempt_id,
 namespace,secret_name,secret_uid,secret_resource_version,content_sha256,authentication_sha256,recorded_at)
VALUES(@organization_id::uuid,@connection_id::uuid,@identity_id::uuid,@setup_id::uuid,@attempt_id::uuid,
 @namespace,@name,@uid,@resource_version,@content_digest,@authentication_digest,clock_timestamp())
ON CONFLICT(identity_revision_id) DO NOTHING
RETURNING namespace,secret_name,secret_uid,secret_resource_version,content_sha256,authentication_sha256
)
SELECT namespace,secret_name,secret_uid,secret_resource_version,content_sha256,authentication_sha256 FROM inserted
UNION ALL
SELECT namespace,secret_name,secret_uid,secret_resource_version,content_sha256,authentication_sha256
FROM control_plane.ssh_identity_material_revisions WHERE identity_revision_id=@identity_id::uuid
 AND NOT EXISTS(SELECT 1 FROM inserted);
