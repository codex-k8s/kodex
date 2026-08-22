-- name: platform__commands_createagent_2 :exec
INSERT INTO control_plane.instruction_versions(ref,organization_id,agent_id,version_number,state,content,digest,created_by,published_at) VALUES($1,$2::uuid,$3::uuid,1,'PUBLISHED',$4,$5,$6::uuid,$7)
