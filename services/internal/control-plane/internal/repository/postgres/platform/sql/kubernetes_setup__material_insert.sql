-- name: kubernetes_setup__material_insert :exec
INSERT INTO control_plane.kubernetes_identity_material_revisions(
 organization_id,identity_revision_id,purpose,secret_namespace,secret_name,secret_uid,secret_resource_version,content_sha256)
VALUES(@organization_id::uuid,@identity_id::uuid,'KUBERNETES_TARGET',@namespace,@name,@uid::uuid,@resource_version,@content_digest)
ON CONFLICT(identity_revision_id) DO NOTHING;
