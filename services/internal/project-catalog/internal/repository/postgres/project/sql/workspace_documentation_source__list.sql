-- name: workspace_documentation_source__list :many
WITH active_policy AS (
    SELECT id
    FROM project_catalog_services_policies
    WHERE project_id = @project_id::uuid
      AND validation_status = 'valid'
      AND projection_status IN ('synced', 'overridden')
    ORDER BY policy_version DESC, imported_at DESC, id
    LIMIT 1
),
active_descriptors AS (
    SELECT
        sd.repository_id,
        sd.service_key,
        COALESCE(NULLIF(sd.documentation_scope_id, ''), sd.service_key) AS documentation_scope_id
    FROM project_catalog_service_descriptors AS sd
    JOIN active_policy AS ap ON ap.id = sd.services_policy_id
    WHERE sd.project_id = @project_id::uuid
      AND sd.status = 'active'
)
SELECT DISTINCT
    ds.id, ds.repository_id, ds.scope_type, ds.scope_id, ds.local_path, ds.access_mode
FROM project_catalog_documentation_sources AS ds
LEFT JOIN active_descriptors AS sd
    ON sd.documentation_scope_id = ds.scope_id
WHERE ds.project_id = @project_id::uuid
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
      OR sd.service_key = ANY(@service_keys::text[])
  )
ORDER BY ds.scope_type, ds.scope_id, ds.local_path, ds.id;
