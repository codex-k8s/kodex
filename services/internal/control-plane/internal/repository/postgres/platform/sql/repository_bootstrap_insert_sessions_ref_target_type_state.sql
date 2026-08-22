-- name: platform__repository_bootstrap_insert_sessions_ref_target_type_state :exec
INSERT INTO control_plane.sessions
		(ref,organization_id,target_type,target_ref,state,created_by)
		VALUES ($1,$2::uuid,'SYSTEM_ASSISTANT','system-assistant','ACTIVE',$3::uuid)
