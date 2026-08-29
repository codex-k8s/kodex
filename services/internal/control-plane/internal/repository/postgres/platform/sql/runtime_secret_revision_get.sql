-- name: runtime_secret_revision_get :one
SELECT revision, namespace, secret_name, secret_key, secret_uid,
       secret_resource_version, content_sha256
FROM control_plane.runtime_secret_revisions
WHERE secret_id = @secret_id::uuid AND revision = @revision;
