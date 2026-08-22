-- name: platform__configuration_createassistantconversation_insert_sessions_ref_project_id_target_ref :one
INSERT INTO control_plane.sessions(ref,organization_id,project_id,target_type,target_ref,provider_account_id,state,created_by) VALUES($1,$2::uuid,$3::uuid,'SYSTEM_ASSISTANT','system-assistant',$4::uuid,'ACTIVE',$5::uuid) RETURNING id::text
