-- name: organization_runtime_image_mark_custom :exec
UPDATE control_plane.image_artifacts
SET signature_identity = 'component-owner-admitted-custom'
WHERE id = $1::uuid;
