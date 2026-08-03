-- name: OwnerGateByDeliveryClaimKey
-- Только поиск без блокировки. До чтения command receipt сервис обязан
-- заблокировать точный owner graph, а затем OwnerGate.
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
  AND spec ->> 'deliveryClaimKeySha256' = @delivery_claim_key_sha256
ORDER BY created_at, id
LIMIT 1
