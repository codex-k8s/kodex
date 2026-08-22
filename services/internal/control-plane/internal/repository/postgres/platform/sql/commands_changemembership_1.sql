-- name: platform__commands_changemembership_1 :one
INSERT INTO control_plane.memberships(ref,organization_id,project_id,subject_id,role,permissions,active) SELECT $1,$2::uuid,$3::uuid,s.id,$5,$6,true FROM control_plane.subjects s WHERE s.organization_id=$2::uuid AND s.ref=$4 RETURNING ref,role,permissions,active,version
