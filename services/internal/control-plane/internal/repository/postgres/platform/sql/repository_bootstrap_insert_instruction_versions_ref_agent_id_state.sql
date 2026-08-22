-- name: platform__repository_bootstrap_insert_instruction_versions_ref_agent_id_state :exec
INSERT INTO control_plane.instruction_versions
		(ref,organization_id,agent_id,version_number,state,content,digest,core,published_at)
		VALUES ($1,$2::uuid,$3::uuid,1,'PUBLISHED',$4,$5,true,clock_timestamp())
