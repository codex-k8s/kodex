-- +goose Up
CREATE TABLE integration_gateway.runtime_credential_fence (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    current_high_watermark bigint NOT NULL CHECK (current_high_watermark >= 0),
    served_readback_generation bigint NOT NULL CHECK (served_readback_generation >= 0),
    updated_at timestamptz NOT NULL
);
INSERT INTO integration_gateway.runtime_credential_fence (
    singleton, current_high_watermark, served_readback_generation, updated_at
) VALUES (true, 0, 0, clock_timestamp());

GRANT SELECT, UPDATE ON integration_gateway.runtime_credential_fence
    TO integration_gateway_migrator;
ALTER TABLE integration_gateway.runtime_credential_fence
    OWNER TO integration_gateway_owner;

-- +goose Down
-- Forward-only: credential high-watermark нельзя удалять или уменьшать.
SELECT 1;
