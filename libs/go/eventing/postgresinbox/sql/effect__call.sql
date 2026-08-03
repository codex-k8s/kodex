-- name: effect__call :one
SELECT __postgresinbox_effect_function__(@effect_input::jsonb)::jsonb;
