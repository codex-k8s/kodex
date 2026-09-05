-- name: skill_bundle_set_draft :exec
UPDATE control_plane.skill_bundles SET draft_revision_id=$3::uuid,version=version+$4,updated_at=clock_timestamp()
WHERE organization_id=$1::uuid AND id=$2::uuid;
