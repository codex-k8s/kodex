-- name: assistant_configuration_catalog__profiles :many
SELECT stable_key,name,provider,model,version
FROM control_plane.runtime_profiles
WHERE enabled AND stable_key ~ '^[A-Za-z0-9_-]{8,128}$'
 AND (@query='' OR strpos(lower(name),lower(@query))>0 OR strpos(lower(stable_key),lower(@query))>0)
ORDER BY name,stable_key LIMIT 11 OFFSET @offset;
