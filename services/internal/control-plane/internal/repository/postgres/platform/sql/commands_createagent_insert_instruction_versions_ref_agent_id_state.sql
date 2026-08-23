-- name: commands_createagent_insert_instruction_versions_ref_agent_id_state :exec
INSERT INTO control_plane.instruction_versions(ref,organization_id,agent_id,version_number,state,content,digest,created_by,published_at) VALUES($1,$2::uuid,$3::uuid,1,'PUBLISHED',$4,$5,$6::uuid,$7)
