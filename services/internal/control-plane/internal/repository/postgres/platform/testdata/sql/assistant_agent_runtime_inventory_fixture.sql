-- name: assistant_agent_runtime_inventory_fixture :one
UPDATE control_plane.image_artifacts
SET tool_inventory_json=@inventory_json,
    tool_inventory_sha256=@inventory_sha256
WHERE organization_id=@organization_id::uuid
  AND ref=@artifact_ref
  AND manifest_digest=@manifest_digest
  AND provenance_sha256=@provenance_sha256
  AND signature_identity='platform-owned-bootstrap'
  AND admission_verdict='ACCEPTED'
  AND promotion_state='PROMOTED'
  AND tool_inventory_json=''
  AND tool_inventory_sha256=''
RETURNING ref;
