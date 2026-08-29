-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.installation
    ADD COLUMN authority_proof_sequence bigint NOT NULL DEFAULT 0
        CHECK (authority_proof_sequence >= 0);

UPDATE control_plane.installation
SET authority_proof_sequence = platform_sequence
WHERE singleton;

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

ALTER TABLE control_plane.installation
    DROP COLUMN authority_proof_sequence;

RESET ROLE;
