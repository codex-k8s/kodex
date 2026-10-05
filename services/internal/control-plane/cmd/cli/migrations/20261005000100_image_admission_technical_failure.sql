-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.image_artifacts
    DROP CONSTRAINT image_artifacts_admission_state_check,
    ADD CONSTRAINT image_artifacts_admission_state_check
        CHECK (admission_state IN ('PENDING', 'CLAIMED', 'ACCEPTED', 'REJECTED', 'FAILED')),
    ADD COLUMN admission_failure_authority_generation bigint NOT NULL DEFAULT 0 CHECK (admission_failure_authority_generation >= 0),
    ADD COLUMN admission_failure_code text NOT NULL DEFAULT ''
        CHECK (admission_failure_code IN ('', 'ADMISSION_EVIDENCE_ENTRY_EXCEEDS_BOUND',
            'ADMISSION_EVIDENCE_EXCEEDS_BOUND', 'ADMISSION_WORKER_FAILED', 'ADMISSION_LEASE_EXPIRED')),
    ADD CONSTRAINT image_artifacts_admission_failure_check
        CHECK ((admission_state = 'FAILED') = (admission_failure_code <> '') AND
          (admission_state <> 'FAILED' OR admission_failure_authority_generation > 0));
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $ BEGIN
    RAISE EXCEPTION 'image admission technical failure migration is forward-only';
END $;
-- +goose StatementEnd
