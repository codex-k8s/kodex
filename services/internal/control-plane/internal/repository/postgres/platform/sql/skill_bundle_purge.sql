-- name: skill_bundle_purge :exec
UPDATE control_plane.skill_bundle_revisions SET files='[]'::jsonb
WHERE organization_id=$1::uuid AND bundle_id=$2::uuid AND files<>'[]'::jsonb;
