SELECT
    id::text, organization_id::text, project_id::text,
    coalesce(parent_id::text, ''), owner_actor_id::text,
    kind, name, state, version, spec, created_at, updated_at
FROM control_plane.resources
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND owner_actor_id = @owner_actor_id::uuid
  AND kind = 'IMAGE_ARTIFACT'
  AND state = 'ACTIVE'
  AND spec ->> 'specSha256' = @spec_sha256
  AND (spec ->> 'policyRevision')::bigint = @policy_revision
  AND spec ->> 'policySha256' = @policy_sha256
  AND spec ->> 'admissionVerdict' = 'ACCEPTED'
  AND coalesce(spec ->> 'promotedReference', '') <> ''
  AND coalesce(spec ->> 'promotionReadbackSha256', '') <> ''
ORDER BY version DESC, updated_at DESC, id
LIMIT 1
