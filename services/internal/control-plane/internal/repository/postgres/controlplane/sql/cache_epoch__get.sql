SELECT epoch
FROM control_plane.cache_epochs
WHERE organization_id = @organization_id::uuid
  AND scope_key = CASE WHEN @project_id = '' THEN 'tenant' ELSE @project_id END
