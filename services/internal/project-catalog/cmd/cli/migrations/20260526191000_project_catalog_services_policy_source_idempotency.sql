-- +goose Up
CREATE INDEX project_catalog_services_policies_source_replay_idx
    ON project_catalog_services_policies (
        project_id,
        source_repository_id,
        source_path,
        source_commit_sha,
        imported_at DESC
    )
    WHERE validation_status = 'valid'
      AND projection_status IN ('synced', 'overridden');

-- +goose Down
DROP INDEX IF EXISTS project_catalog_services_policies_source_replay_idx;
