-- name: kubernetes_setup__material_read :one
SELECT secret_namespace,secret_name,secret_uid::text,secret_resource_version,content_sha256
FROM control_plane.kubernetes_identity_material_revisions
WHERE organization_id=@organization_id::uuid AND identity_revision_id=@identity_id::uuid AND purpose='KUBERNETES_TARGET';
