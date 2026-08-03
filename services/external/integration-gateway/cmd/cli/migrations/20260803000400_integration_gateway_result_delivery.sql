-- +goose Up
ALTER TABLE integration_gateway.results
    ADD COLUMN delivery_version bigint NOT NULL DEFAULT 1 CHECK (delivery_version > 0),
    ADD COLUMN delivery_fence bigint NOT NULL DEFAULT 1 CHECK (delivery_fence > 0),
    ADD COLUMN acknowledged_at timestamptz;

CREATE TABLE integration_gateway.result_delivery_receipts (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    invocation_id text NOT NULL REFERENCES integration_gateway.results(invocation_id),
    idempotency_hash text NOT NULL CHECK (idempotency_hash ~ '^[0-9a-f]{64}$'),
    request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    delivery_version bigint NOT NULL CHECK (delivery_version > 0),
    delivery_fence bigint NOT NULL CHECK (delivery_fence > 0),
    acknowledged_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_id, idempotency_hash)
);

ALTER TABLE integration_gateway.result_delivery_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_gateway.result_delivery_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY result_delivery_receipts_runtime_scope
    ON integration_gateway.result_delivery_receipts
    USING (integration_gateway.scope_matches(tenant_id, project_id))
    WITH CHECK (integration_gateway.scope_matches(tenant_id, project_id));
GRANT SELECT, INSERT ON integration_gateway.result_delivery_receipts TO integration_gateway_runtime;
GRANT SELECT, UPDATE ON integration_gateway.results TO integration_gateway_runtime;
ALTER TABLE integration_gateway.result_delivery_receipts OWNER TO integration_gateway_owner;

-- +goose Down
-- Forward-only migration: delivery receipts и fences не открываются назад.
SELECT 1;
