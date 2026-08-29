-- name: runtime_secret_revision_insert :exec
INSERT INTO control_plane.runtime_secret_revisions
  (ref, secret_id, revision, namespace, secret_name, secret_key, secret_uid, secret_resource_version, content_sha256)
VALUES
  (@ref, @secret_id::uuid, @revision, @namespace, @secret_name, @secret_key, @secret_uid, @secret_resource_version, @content_sha256);
