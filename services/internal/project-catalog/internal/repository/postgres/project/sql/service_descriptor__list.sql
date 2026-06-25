-- name: service_descriptor__list :many
WITH active_policy AS (
    SELECT id
    FROM project_catalog_services_policies
    WHERE project_id = @project_id::uuid
      AND (@services_policy_id::uuid IS NULL OR id = @services_policy_id::uuid)
      AND validation_status = 'valid'
      AND projection_status IN ('synced', 'overridden')
    ORDER BY policy_version DESC, imported_at DESC, id
    LIMIT 1
)
SELECT
    sd.id, sd.project_id, sd.services_policy_id, sd.repository_id, sd.service_key,
    sd.display_name, sd.kind, sd.root_path, sd.documentation_scope_id,
    sd.depends_on_service_keys, sd.status, sd.version, sd.created_at, sd.updated_at
FROM project_catalog_service_descriptors AS sd
JOIN active_policy AS ap ON ap.id = sd.services_policy_id
WHERE sd.project_id = @project_id::uuid
  AND (@repository_id::uuid IS NULL OR sd.repository_id = @repository_id::uuid)
  AND (cardinality(@service_keys::text[]) = 0 OR sd.service_key = ANY(@service_keys::text[]))
  AND (cardinality(@statuses::text[]) = 0 OR sd.status = ANY(@statuses::text[]))
ORDER BY sd.service_key, sd.id
LIMIT @limit::integer OFFSET @offset::integer;
