-- name: interaction_find_message_receipt :one
SELECT
    receipt.outcome,
    COALESCE(run.ref, ''),
    COALESCE(gate.ref, '')
FROM control_plane.interaction_message_receipts receipt
LEFT JOIN control_plane.runs run ON run.id = receipt.root_run_id
LEFT JOIN control_plane.owner_gates gate ON gate.id = receipt.gate_id
WHERE receipt.organization_id = @organization_id::uuid
  AND receipt.connection_id = @connection_id::uuid
  AND receipt.external_event_digest = @external_event_digest
  AND receipt.subject_id = @subject_id::uuid
  AND receipt.identity_id = @identity_id::uuid
LIMIT 1
