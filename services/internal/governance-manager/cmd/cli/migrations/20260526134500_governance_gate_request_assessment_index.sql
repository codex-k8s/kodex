-- +goose Up
CREATE INDEX governance_manager_gate_requests_assessment_updated_idx
    ON governance_manager_gate_requests (risk_assessment_id, updated_at DESC, id)
    WHERE risk_assessment_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS governance_manager_gate_requests_assessment_updated_idx;
