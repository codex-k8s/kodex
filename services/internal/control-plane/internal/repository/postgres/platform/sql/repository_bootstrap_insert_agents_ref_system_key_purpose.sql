-- name: platform__repository_bootstrap_insert_agents_ref_system_key_purpose :one
INSERT INTO control_plane.agents
		(ref,organization_id,system_key,name,purpose,role_description,runtime_key,state,enabled)
		VALUES ($1,$2::uuid,'system-assistant','Системный помощник','Настраивает платформу и объясняет её состояние',
		'Встроенный неудаляемый помощник с типизированными tools',$3,'READY',true) RETURNING id::text
