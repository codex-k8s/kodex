SELECT
    id::text, organization_id::text, project_id::text,
    coalesce(parent_id::text, ''), owner_actor_id::text,
    kind, name, state, version, spec, created_at, updated_at
FROM control_plane.resources
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND kind = 'IMAGE_ARTIFACT'
  AND state = 'WAITING_EXTERNAL'
  AND spec ->> 'admissionVerdict' = 'ACCEPTED'
  AND (spec ->> 'policyRevision')::bigint = @policy_revision
  AND spec ->> 'policySha256' = @policy_sha256
  AND coalesce(spec ->> 'promotedReference', '') = ''
  AND (
      (
          coalesce(spec ->> 'promotionAuthorizationTokenSha256', '') = ''
          AND (
              coalesce(spec ->> 'promotionClaimJtiSha256', '') = ''
              OR (spec ->> 'promotionClaimExpiresAt')::timestamptz <= @now
          )
      )
      OR (
          coalesce(spec ->> 'promotionAuthorizationTokenSha256', '') <> ''
          AND (spec ->> 'promotionAuthorizationExpiresAt')::timestamptz <= @now
      )
  )
ORDER BY created_at, id
FOR UPDATE SKIP LOCKED
LIMIT 1
