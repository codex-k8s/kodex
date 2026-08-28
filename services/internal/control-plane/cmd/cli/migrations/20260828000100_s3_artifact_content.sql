-- +goose Up
SET ROLE control_plane_owner;

-- Прототип до production rollout не имеет поддерживаемых legacy-данных:
-- bounded bytea storage заменяется единым S3-backed metadata contract.
DROP TABLE control_plane.artifact_content;

CREATE TABLE control_plane.artifact_content (
    artifact_id uuid PRIMARY KEY REFERENCES control_plane.artifacts(id) ON DELETE CASCADE,
    object_key text NOT NULL UNIQUE CHECK (char_length(object_key) BETWEEN 1 AND 1024),
    object_version text NOT NULL CHECK (char_length(object_version) <= 1024),
    object_etag text NOT NULL CHECK (char_length(object_etag) <= 256),
    digest text NOT NULL CHECK (digest ~ '^sha256:[a-f0-9]{64}$'),
    size_bytes bigint NOT NULL CHECK (size_bytes BETWEEN 0 AND 1073741824),
    stored_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;
DROP TABLE control_plane.artifact_content;
CREATE TABLE control_plane.artifact_content (
    artifact_id uuid PRIMARY KEY REFERENCES control_plane.artifacts(id) ON DELETE CASCADE,
    body bytea NOT NULL CHECK (octet_length(body) <= 16777216)
);
RESET ROLE;
