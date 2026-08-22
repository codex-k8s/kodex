-- name: platform__repository_bootstrap_insert_agents_ref_system_key_purpose :one
INSERT INTO control_plane.agents
		(ref,organization_id,role_definition_id,system_key,name,purpose,role_description,runtime_key,state,enabled)
		VALUES ($1,$2::uuid,$3::uuid,'system-assistant','i18n:SYSTEM_ASSISTANT_NAME','i18n:SYSTEM_ASSISTANT_PURPOSE',
		'i18n:SYSTEM_ASSISTANT_ROLE_DESCRIPTION',$4,'READY',true) RETURNING id::text
