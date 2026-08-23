-- name: commands_createproject_insert_memberships_ref_project_id_role :exec
INSERT INTO control_plane.memberships(ref,organization_id,project_id,subject_id,role,permissions) VALUES($1,$2::uuid,(SELECT id FROM control_plane.projects WHERE ref=$3),$4::uuid,'OWNER',$5)
