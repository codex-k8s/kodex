-- +goose Up
ALTER TABLE runtime_manager_build_contexts
    ADD COLUMN manifest_bundle_digests_json jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE runtime_manager_build_contexts
    ADD CONSTRAINT runtime_manager_build_contexts_manifest_bundle_digests_chk
        CHECK (jsonb_typeof(manifest_bundle_digests_json) = 'object');

-- +goose Down
ALTER TABLE runtime_manager_build_contexts
    DROP CONSTRAINT runtime_manager_build_contexts_manifest_bundle_digests_chk,
    DROP COLUMN manifest_bundle_digests_json;
