-- name: OwnerGateNextDelivery
SELECT
    id,
    organization_id,
    project_id,
    parent_id,
    owner_actor_id,
    kind,
    name,
    state,
    version,
    spec,
    created_at,
    updated_at,
    deleted_at
FROM control_plane.resources
WHERE organization_id = @organization_id::uuid
  AND project_id = @project_id::uuid
  AND kind = 'OWNER_GATE'
  AND state = 'WAITING_OWNER'
  AND (spec ->> 'expiresAt')::timestamptz > clock_timestamp()
  AND coalesce(spec ->> 'mattermostPostId', '') = ''
  AND (
      coalesce(spec ->> 'deliveryClaimTokenSha256', '') = ''
      OR coalesce(
          (spec ->> 'deliveryClaimExpiresAt')::timestamptz,
          '-infinity'::timestamptz
      ) <= @now
  )
ORDER BY created_at, id
LIMIT 1
