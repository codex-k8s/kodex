-- name: platform__commands_createproject_2 :exec
INSERT INTO control_plane.memberships(ref,organization_id,project_id,subject_id,role,permissions) VALUES($1,$2::uuid,(SELECT id FROM control_plane.projects WHERE ref=$3),$4::uuid,'OWNER',$5)
