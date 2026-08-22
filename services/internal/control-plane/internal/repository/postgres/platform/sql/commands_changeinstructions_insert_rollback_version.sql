-- name: commands_changeinstructions_insert_rollback_version :exec
INSERT INTO control_plane.instruction_versions(ref,organization_id,agent_id,version_number,state,content,digest,parent_ref,created_by,published_at) VALUES($1,$2::uuid,$3::uuid,$4,'PUBLISHED',$5,$6,$7,$8::uuid,clock_timestamp())
