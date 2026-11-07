-- name: personal_credential__insert_revision :one
INSERT INTO control_plane.integration_personal_credential_revisions(
    ref, organization_id, binding_id, revision, secret_ref, secret_uid, secret_resource_version, content_sha256)
SELECT @ref, @organization_id::uuid, @binding_id::uuid, COALESCE(max(revision), 0) + 1,
       @secret_ref, @secret_uid::uuid, @secret_resource_version, @content_sha256
FROM control_plane.integration_personal_credential_revisions
WHERE organization_id = @organization_id::uuid AND binding_id = @binding_id::uuid
RETURNING id::text, ref, revision, secret_ref, secret_uid::text, secret_resource_version, content_sha256, created_at
