-- name: transport__readback :one
SELECT ssl, version, cipher
FROM pg_catalog.pg_stat_ssl
WHERE pid = pg_catalog.pg_backend_pid();
