-- name: backup__server_version :one
SELECT pg_catalog.current_setting('server_version_num');
