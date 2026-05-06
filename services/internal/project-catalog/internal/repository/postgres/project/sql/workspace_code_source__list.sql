-- name: workspace_code_source__list :many
WITH active_policy AS (
    SELECT id
    FROM project_catalog_services_policies
    WHERE project_id = @project_id::uuid
      AND validation_status = 'valid'
      AND projection_status IN ('synced', 'overridden')
    ORDER BY policy_version DESC, imported_at DESC, id
    LIMIT 1
),
selected_service_repositories AS (
    SELECT DISTINCT sd.repository_id
    FROM project_catalog_service_descriptors AS sd
    JOIN active_policy AS ap ON ap.id = sd.services_policy_id
    WHERE sd.project_id = @project_id::uuid
      AND sd.status = 'active'
      AND sd.repository_id IS NOT NULL
      AND (
          cardinality(@service_keys::text[]) = 0
          OR sd.service_key = ANY(@service_keys::text[])
      )
)
SELECT
    r.id, r.provider, r.provider_owner, r.provider_name, r.default_branch
FROM project_catalog_repositories AS r
WHERE r.project_id = @project_id::uuid
  AND r.status = 'active'
  AND (cardinality(@repository_ids::uuid[]) = 0 OR r.id = ANY(@repository_ids::uuid[]))
  AND (
      cardinality(@service_keys::text[]) = 0
      OR r.id IN (SELECT repository_id FROM selected_service_repositories)
  )
ORDER BY r.provider, r.provider_owner, r.provider_name, r.id;
