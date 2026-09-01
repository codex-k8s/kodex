-- name: restore__database_exists :one
SELECT EXISTS (
    SELECT 1
    FROM pg_catalog.pg_database
    WHERE datname = @database
);
