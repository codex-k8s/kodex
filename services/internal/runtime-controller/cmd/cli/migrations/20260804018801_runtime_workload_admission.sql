-- +goose Up
RESET ROLE;
SET ROLE runtime_controller_owner;
SET ROLE runtime_controller_owner;
SET search_path TO runtime_controller, pg_catalog;

CREATE TABLE runtime_workload_ticket_admissions (
    request_uid uuid PRIMARY KEY,
    ticket_id bytea NOT NULL UNIQUE CHECK (octet_length(ticket_id) = 32),
    pod_sha256 bytea NOT NULL CHECK (octet_length(pod_sha256) = 32),
    execution_id uuid NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT transaction_timestamp(),
    CHECK (expires_at > created_at)
);

REVOKE ALL ON runtime_workload_ticket_admissions FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON runtime_workload_ticket_admissions TO runtime_workload_admission_g1;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$
BEGIN
    RAISE EXCEPTION 'runtime workload admission receipts are forward-only' USING ERRCODE = '0A000';
END
$forward_only$;
-- +goose StatementEnd
