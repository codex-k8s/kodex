-- name: context_binding_skill_files :one
SELECT revision.files
FROM control_plane.skill_bundle_revisions revision
JOIN control_plane.skill_bundles bundle ON bundle.id=revision.bundle_id
WHERE bundle.organization_id=$1::uuid AND bundle.ref=$2 AND revision.ref=$3;
