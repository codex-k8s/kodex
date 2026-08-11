-- +goose Up
RESET ROLE;
SET ROLE runtime_controller_owner;
SET ROLE runtime_controller_owner;
SET search_path TO runtime_controller, pg_catalog;

ALTER TABLE runtime_workload_ticket_admissions
	ADD COLUMN resource_key text;
UPDATE runtime_workload_ticket_admissions
	SET resource_key = 'pod/' || execution_id::text
	WHERE resource_key IS NULL;
ALTER TABLE runtime_workload_ticket_admissions
	ALTER COLUMN resource_key SET NOT NULL,
	ADD CONSTRAINT runtime_workload_resource_key_valid
		CHECK (length(resource_key) BETWEEN 5 AND 253 AND resource_key = btrim(resource_key));
ALTER TABLE runtime_workload_ticket_admissions
	DROP CONSTRAINT runtime_workload_ticket_admissions_ticket_id_key;
ALTER TABLE runtime_workload_ticket_admissions
	ADD CONSTRAINT runtime_workload_ticket_resource_unique UNIQUE (ticket_id, resource_key);

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $forward_only$
BEGIN
	RAISE EXCEPTION 'runtime workload resource admission receipts are forward-only' USING ERRCODE = '0A000';
END
$forward_only$;
-- +goose StatementEnd
