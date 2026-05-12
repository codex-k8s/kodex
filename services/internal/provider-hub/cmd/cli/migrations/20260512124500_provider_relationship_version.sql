-- +goose Up
ALTER TABLE provider_hub_relationships
    ADD COLUMN version bigint NOT NULL DEFAULT 1;

ALTER TABLE provider_hub_relationships
    ADD CONSTRAINT provider_hub_relationships_version_chk CHECK (version > 0);

-- +goose Down
ALTER TABLE provider_hub_relationships
    DROP CONSTRAINT IF EXISTS provider_hub_relationships_version_chk;

ALTER TABLE provider_hub_relationships
    DROP COLUMN IF EXISTS version;
