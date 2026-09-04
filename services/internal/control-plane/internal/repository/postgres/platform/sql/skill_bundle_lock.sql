-- name: skill_bundle_lock :one
SELECT id::text,version,state FROM control_plane.skill_bundles
WHERE organization_id=$1::uuid AND ref=$2 FOR UPDATE;
