-- name: workspace_guidance_ref__list :many
SELECT DISTINCT scope_id
FROM project_catalog_documentation_sources
WHERE project_id = @project_id::uuid
  AND status = 'active'
  AND scope_type = 'guidance_ref'
ORDER BY scope_id;
