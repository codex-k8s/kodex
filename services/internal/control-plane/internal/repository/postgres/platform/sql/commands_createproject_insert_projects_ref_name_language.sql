-- name: commands_createproject_insert_projects_ref_name_language :one
INSERT INTO control_plane.projects(ref,organization_id,name,purpose,language,created_by) VALUES($1,$2::uuid,$3,$4,$5,$6::uuid) RETURNING ref,name,purpose,language,lifecycle,version,created_at,updated_at
