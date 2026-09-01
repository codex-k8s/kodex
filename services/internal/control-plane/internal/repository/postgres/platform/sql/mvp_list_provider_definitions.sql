-- name: mvp_list_provider_definitions :many
SELECT stable_key, name, capabilities, enabled
FROM control_plane.provider_definitions
WHERE (@query = '' OR stable_key ILIKE '%' || @query || '%' OR name ILIKE '%' || @query || '%')
  AND (@cursor_key = '' OR stable_key > @cursor_key)
ORDER BY stable_key
LIMIT @page_size;
