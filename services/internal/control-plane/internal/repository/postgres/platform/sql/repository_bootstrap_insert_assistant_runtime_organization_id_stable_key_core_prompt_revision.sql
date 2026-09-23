-- name: repository_bootstrap_insert_assistant_runtime_organization_id_stable_key_core_prompt_revision :exec
INSERT INTO control_plane.assistant_runtime
		(organization_id,agent_id,stable_key,core_prompt_ref,core_prompt_revision,runtime_state,
		runtime_revision,desired_runtime_revision,system_session_ref,resource_limits)
		VALUES ($1::uuid,$2::uuid,'system-assistant',$3,$4,
            CASE WHEN $5 = '' THEN 'UNAVAILABLE' ELSE 'STARTING' END,
            '',$4,NULLIF($5,''),$6)
