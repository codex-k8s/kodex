-- +goose Up
SET ROLE control_plane_owner;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'control_plane'
          AND table_name = 'artifact_content'
          AND column_name = 'body'
    ) THEN
        RAISE EXCEPTION 'legacy artifact bytea storage remains; apply the S3 authority migration first';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE control_plane.artifacts
    DROP CONSTRAINT artifacts_size_bytes_check,
    ADD CONSTRAINT artifacts_size_bytes_check
        CHECK (size_bytes BETWEEN 0 AND 536870912);

ALTER TABLE control_plane.artifact_content
    DROP CONSTRAINT artifact_content_size_bytes_check,
    ADD CONSTRAINT artifact_content_size_bytes_check
        CHECK (size_bytes BETWEEN 0 AND 536870912);

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

ALTER TABLE control_plane.artifacts
    DROP CONSTRAINT artifacts_size_bytes_check,
    ADD CONSTRAINT artifacts_size_bytes_check
        CHECK (size_bytes BETWEEN 0 AND 1073741824);

ALTER TABLE control_plane.artifact_content
    DROP CONSTRAINT artifact_content_size_bytes_check,
    ADD CONSTRAINT artifact_content_size_bytes_check
        CHECK (size_bytes BETWEEN 0 AND 1073741824);

RESET ROLE;
