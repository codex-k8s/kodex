-- name: workspace_documentation_source__list :many
SELECT DISTINCT
    ds.id, ds.repository_id, ds.scope_type, ds.scope_id, ds.local_path, ds.access_mode
FROM project_catalog_documentation_sources AS ds
LEFT JOIN project_catalog_service_descriptors AS sd
    ON sd.project_id = ds.project_id
   AND sd.service_key = ds.scope_id
WHERE ds.project_id = @project_id
  AND ds.status = 'active'
  AND ds.scope_type <> 'guidance_ref'
  AND (
      cardinality(@repository_ids::uuid[]) = 0
      OR ds.repository_id = ANY(@repository_ids::uuid[])
      OR sd.repository_id = ANY(@repository_ids::uuid[])
  )
  AND (
      cardinality(@service_keys::text[]) = 0
      OR ds.scope_type <> 'service'
      OR ds.scope_id = ANY(@service_keys::text[])
  )
ORDER BY ds.scope_type, ds.scope_id, ds.local_path, ds.id;
