-- name: platform__commands_launchrun_insert_sessions_ref_project_id_target_ref :one
INSERT INTO control_plane.sessions(ref,organization_id,project_id,target_type,target_ref,provider_account_id,state,created_by) VALUES($1,$2::uuid,$3::uuid,$4,$5,$6::uuid,'ACTIVE',$7::uuid) RETURNING id::text
