-- name: opencode_native_visibility_definition :one
SELECT pg_get_functiondef('control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)'::regprocedure);
