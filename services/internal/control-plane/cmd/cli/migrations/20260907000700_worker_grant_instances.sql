-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.worker_grant_instance_high_watermarks (
    workload_id text NOT NULL REFERENCES control_plane.worker_grant_high_watermarks(workload_id),
    instance_id uuid NOT NULL CHECK (instance_id <> '00000000-0000-0000-0000-000000000000'),
    credential_generation bigint NOT NULL CHECK (credential_generation > 0),
    revision bigint NOT NULL CHECK (revision > 0),
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > issued_at),
    envelope_sha256 text NOT NULL CHECK (envelope_sha256 ~ '^[a-f0-9]{64}$'),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (workload_id, instance_id)
);

REVOKE DELETE ON control_plane.worker_grant_instance_high_watermarks FROM control_plane_runtime;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'worker grant instance watermarks are forward-only';
END $$;
-- +goose StatementEnd
