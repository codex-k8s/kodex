-- name: platform__commands_launchrun_3 :one
INSERT INTO control_plane.sessions(ref,organization_id,project_id,target_type,target_ref,state,created_by) VALUES($1,$2::uuid,$3::uuid,$4,$5,'ACTIVE',$6::uuid) RETURNING id::text
