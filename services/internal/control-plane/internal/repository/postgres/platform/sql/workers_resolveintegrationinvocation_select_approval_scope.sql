-- name: workers_resolveintegrationinvocation_select_approval_scope :one
SELECT id::text
FROM control_plane.integration_approval_scopes
WHERE organization_id=$1::uuid AND project_id=$2::uuid
  AND connection_id=$3::uuid AND grant_id=$4::uuid
  AND root_run_id=$5::uuid AND agent_id=$6::uuid
  AND capability_key=$7 AND grant_version=$8
  AND definition_digest=$9 AND input_schema_digest=$10
  AND scope_paths=$11::text[] AND scope_digest=$12
  AND revoked_at IS NULL AND expires_at>clock_timestamp()
  AND reserved_effects<max_effects
ORDER BY created_at DESC,id DESC
LIMIT 1
FOR UPDATE
