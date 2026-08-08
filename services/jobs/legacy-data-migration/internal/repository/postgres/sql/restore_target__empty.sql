-- name: restore_target__empty :one
WITH user_namespaces AS (
    SELECT oid, nspname
    FROM pg_catalog.pg_namespace
    WHERE nspname <> 'information_schema'
      AND nspname !~ '^pg_'
), user_objects AS (
    SELECT class.oid
    FROM pg_catalog.pg_class AS class
    JOIN user_namespaces AS namespace ON namespace.oid = class.relnamespace
    UNION ALL
    SELECT procedure.oid
    FROM pg_catalog.pg_proc AS procedure
    JOIN user_namespaces AS namespace ON namespace.oid = procedure.pronamespace
    UNION ALL
    SELECT type.oid
    FROM pg_catalog.pg_type AS type
    JOIN user_namespaces AS namespace ON namespace.oid = type.typnamespace
    WHERE type.typtype IN ('d', 'e', 'm', 'r')
    UNION ALL
    SELECT namespace.oid
    FROM user_namespaces AS namespace
    WHERE namespace.nspname <> 'public'
)
SELECT current_database(), count(*)
FROM user_objects;
