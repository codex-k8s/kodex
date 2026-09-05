-- name: skill_bundle_get :one
SELECT id::text,project_id::text,COALESCE(current_revision_id::text,''),COALESCE(draft_revision_id::text,''),projection
FROM control_plane.skill_bundle_projection WHERE organization_id=$1::uuid AND ref=$2;
