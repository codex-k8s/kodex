-- name: skill_bundle_insert :one
INSERT INTO control_plane.skill_bundles(ref,organization_id,project_id,created_by)
SELECT $2,$1::uuid,id,$4::uuid FROM control_plane.projects
WHERE organization_id=$1::uuid AND ref=$3 AND lifecycle='ACTIVE'
RETURNING id::text;
