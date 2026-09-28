-- name: assistant_integration_definitions :many
SELECT stable_key, name, description, category, adapter,
       COALESCE(credential_secret_key, ''), configuration_schema, capabilities, origin
FROM control_plane.integration_definitions
WHERE enabled
  AND (@query = '' OR stable_key ILIKE '%' || @query || '%'
       OR name ILIKE '%' || @query || '%'
       OR description ILIKE '%' || @query || '%')
ORDER BY stable_key
LIMIT @limit OFFSET @offset;
