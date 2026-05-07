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
        COALESCE(NULLIF(sd.documentation_scope_id, ''), sd.service_key) AS documentation_scope_id,
        sd.depends_on_service_keys
    FROM project_catalog_service_descriptors AS sd
    JOIN active_policy AS ap ON ap.id = sd.services_policy_id
    WHERE sd.project_id = @project_id::uuid
      AND sd.status = 'active'
),
selected_descriptors AS (
    SELECT *
    FROM active_descriptors
    WHERE cardinality(@service_keys::text[]) = 0
       OR service_key = ANY(@service_keys::text[])
),
selected_dependency_keys AS (
    SELECT DISTINCT dependency_key
    FROM selected_descriptors AS sd
    CROSS JOIN LATERAL unnest(sd.depends_on_service_keys) AS dependency(dependency_key)
),
selected_dependency_scopes AS (
    SELECT DISTINCT ad.documentation_scope_id AS scope_id, ad.repository_id
    FROM active_descriptors AS ad
    JOIN selected_dependency_keys AS dk ON dk.dependency_key = ad.service_key
    UNION
    SELECT DISTINCT ad.service_key AS scope_id, ad.repository_id
    FROM active_descriptors AS ad
    JOIN selected_dependency_keys AS dk ON dk.dependency_key = ad.service_key
)
SELECT DISTINCT
    ds.id, ds.repository_id, ds.scope_type, ds.scope_id, ds.local_path, ds.access_mode
FROM project_catalog_documentation_sources AS ds
WHERE ds.project_id = @project_id::uuid
  AND ds.status = 'active'
  AND ds.scope_type <> 'guidance_ref'
  AND (
      cardinality(@repository_ids::uuid[]) = 0
      OR ds.scope_type = 'project'
      OR ds.repository_id = ANY(@repository_ids::uuid[])
  )
  AND (
      ds.scope_type = 'project'
      OR EXISTS (
          SELECT 1
          FROM selected_descriptors AS sd
          WHERE ds.scope_type = 'service'
            AND ds.scope_id IN (sd.documentation_scope_id, sd.service_key)
      )
      OR EXISTS (
          SELECT 1
          FROM selected_dependency_scopes AS dep
          WHERE ds.scope_type = 'dependency'
            AND dep.scope_id = ds.scope_id
      )
  )
ORDER BY ds.scope_type, ds.scope_id, ds.local_path, ds.id;
