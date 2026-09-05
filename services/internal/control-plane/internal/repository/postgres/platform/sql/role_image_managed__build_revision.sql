-- name: role_image_managed__build_revision :one
SELECT revision.ref
FROM control_plane.image_builds build
JOIN control_plane.managed_role_image_builds mapping ON mapping.build_id = build.id
JOIN control_plane.managed_configuration_revisions revision ON revision.id = mapping.configuration_revision_id AND revision.organization_id = build.organization_id
WHERE build.organization_id = $1::uuid AND build.ref = $2;
