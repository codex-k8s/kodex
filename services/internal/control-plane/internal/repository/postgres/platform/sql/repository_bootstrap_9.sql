-- name: platform__repository_bootstrap_9 :exec
INSERT INTO control_plane.instruction_versions
		(ref,organization_id,agent_id,version_number,state,content,digest,core,published_at)
		VALUES ($1,$2::uuid,$3::uuid,1,'PUBLISHED',$4,$5,true,clock_timestamp())
