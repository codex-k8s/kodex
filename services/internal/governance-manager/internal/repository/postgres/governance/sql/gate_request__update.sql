-- name: gate_request__update :exec
UPDATE governance_manager_gate_requests
SET
    interaction_delivery_ref = @interaction_delivery_ref::jsonb,
    evidence_refs = @evidence_refs::jsonb,
    evidence_summary = @evidence_summary,
    status = @status,
    terminal_actor_ref = @terminal_actor_ref,
    terminal_reason = @terminal_reason,
    terminal_at = @terminal_at,
    version = @version,
    updated_at = @updated_at
WHERE id = @id
  AND version = @previous_version;
