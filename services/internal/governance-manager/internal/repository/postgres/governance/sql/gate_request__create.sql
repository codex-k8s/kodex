-- name: gate_request__create :exec
INSERT INTO governance_manager_gate_requests (
    id, risk_assessment_id, gate_policy_id, target_type, target_ref,
    interaction_delivery_ref, evidence_refs, evidence_summary, status,
    version, created_at, updated_at
) VALUES (
    @id, @risk_assessment_id, @gate_policy_id, @target_type, @target_ref,
    @interaction_delivery_ref::jsonb, @evidence_refs::jsonb, @evidence_summary, @status,
    @version, @created_at, @updated_at
);
