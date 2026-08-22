-- name: platform__commands_createagent_1 :one
INSERT INTO control_plane.agents(ref,organization_id,project_id,name,purpose,role_description,avatar_url,runtime_key,state,enabled,created_by) VALUES($1,$2::uuid,$3::uuid,$4,$5,$6,$7,$8,'READY',true,$9::uuid) RETURNING id::text,ref,name,purpose,role_description,avatar_url,state,enabled,version,created_at,updated_at
