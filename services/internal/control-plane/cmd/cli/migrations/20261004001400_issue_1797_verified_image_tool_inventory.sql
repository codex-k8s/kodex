-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.image_artifacts
    ADD COLUMN tool_inventory_json text NOT NULL DEFAULT '',
    ADD COLUMN tool_inventory_sha256 text NOT NULL DEFAULT '',
    ADD CONSTRAINT image_artifacts_tool_inventory_bounds CHECK (
        (tool_inventory_json = '' AND tool_inventory_sha256 = '') OR
        (octet_length(tool_inventory_json) BETWEEN 1 AND 131072
         AND tool_inventory_sha256 ~ '^[a-f0-9]{64}$'));
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'verified image tool inventory is forward-only';
END $$;
-- +goose StatementEnd
