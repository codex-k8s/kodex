-- name: schema__set_search_path :one
SELECT pg_catalog.set_config(
    'search_path',
    pg_catalog.format('pg_catalog,%I,pg_temp', @schema_name::text),
    true
) AS applied_search_path,
current_user = session_user AS identity_stable;
