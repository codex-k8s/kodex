-- name: configuration_createassistantconversation_insert_assistant_conversations_ref_project_id_title :one
INSERT INTO control_plane.assistant_conversations(ref,organization_id,project_id,session_id,title,state,created_by) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5,'ACTIVE',$6::uuid) RETURNING ref,title,state,version,created_at,updated_at
