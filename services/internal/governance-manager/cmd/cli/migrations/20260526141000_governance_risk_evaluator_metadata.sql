-- +goose Up
ALTER TABLE governance_manager_risk_assessments
    ADD COLUMN risk_profile_id uuid REFERENCES governance_manager_risk_profiles(id),
    ADD COLUMN risk_profile_version bigint,
    ADD COLUMN evaluation_summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN evidence_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD CONSTRAINT governance_manager_risk_assessments_profile_version_fk
        FOREIGN KEY (risk_profile_id, risk_profile_version)
        REFERENCES governance_manager_risk_profile_versions(risk_profile_id, profile_version),
    ADD CONSTRAINT governance_manager_risk_assessments_profile_version_pair_chk
        CHECK ((risk_profile_id IS NULL AND risk_profile_version IS NULL) OR (risk_profile_id IS NOT NULL AND risk_profile_version IS NOT NULL)),
    ADD CONSTRAINT governance_manager_risk_assessments_evaluation_summary_chk
        CHECK (jsonb_typeof(evaluation_summary) = 'object'),
    ADD CONSTRAINT governance_manager_risk_assessments_evidence_refs_chk
        CHECK (jsonb_typeof(evidence_refs) = 'array');

CREATE INDEX governance_manager_risk_assessments_profile_version_idx
    ON governance_manager_risk_assessments (risk_profile_id, risk_profile_version, updated_at DESC, id)
    WHERE risk_profile_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS governance_manager_risk_assessments_profile_version_idx;

ALTER TABLE governance_manager_risk_assessments
    DROP CONSTRAINT IF EXISTS governance_manager_risk_assessments_evaluation_summary_chk,
    DROP CONSTRAINT IF EXISTS governance_manager_risk_assessments_evidence_refs_chk,
    DROP CONSTRAINT IF EXISTS governance_manager_risk_assessments_profile_version_pair_chk,
    DROP CONSTRAINT IF EXISTS governance_manager_risk_assessments_profile_version_fk,
    DROP COLUMN IF EXISTS evaluation_summary,
    DROP COLUMN IF EXISTS evidence_refs,
    DROP COLUMN IF EXISTS risk_profile_version,
    DROP COLUMN IF EXISTS risk_profile_id;
