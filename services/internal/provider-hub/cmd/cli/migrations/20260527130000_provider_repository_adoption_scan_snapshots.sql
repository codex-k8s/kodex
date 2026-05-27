-- +goose Up
ALTER TABLE provider_hub_operations
    DROP CONSTRAINT provider_hub_operations_operation_type_chk,
    ADD CONSTRAINT provider_hub_operations_operation_type_chk
        CHECK (operation_type IN (
            'create_repository',
            'create_issue',
            'update_issue',
            'create_comment',
            'update_comment',
            'create_pull_request',
            'update_pull_request',
            'create_bootstrap_pull_request',
            'create_adoption_pull_request',
            'scan_repository_for_adoption',
            'create_review_signal',
            'update_relationship'
        ));

CREATE TABLE provider_hub_repository_adoption_scan_snapshots (
    id uuid PRIMARY KEY,
    snapshot_key text NOT NULL,
    provider_operation_id uuid NOT NULL REFERENCES provider_hub_operations(id) ON DELETE RESTRICT,
    external_account_id uuid NOT NULL,
    provider_slug text NOT NULL,
    repository_full_name text NOT NULL,
    provider_repository_id text NOT NULL,
    repository_url text NOT NULL,
    default_branch text NOT NULL,
    requested_ref text NOT NULL,
    scanned_ref text NOT NULL,
    head_sha text NOT NULL,
    status text NOT NULL,
    markers_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    file_count bigint NOT NULL DEFAULT 0,
    visible_file_count bigint NOT NULL DEFAULT 0,
    tree_truncated boolean NOT NULL DEFAULT false,
    warnings_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    snapshot_digest text NOT NULL,
    observed_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT provider_hub_repository_adoption_scan_snapshot_key_uniq UNIQUE (snapshot_key),
    CONSTRAINT provider_hub_repository_adoption_scan_operation_uniq UNIQUE (provider_operation_id),
    CONSTRAINT provider_hub_repository_adoption_scan_provider_slug_chk CHECK (provider_slug IN ('github', 'gitlab')),
    CONSTRAINT provider_hub_repository_adoption_scan_status_chk CHECK (status IN ('completed', 'limited', 'needs_review')),
    CONSTRAINT provider_hub_repository_adoption_scan_counts_chk CHECK (file_count >= 0 AND visible_file_count >= 0),
    CONSTRAINT provider_hub_repository_adoption_scan_version_chk CHECK (version > 0),
    CONSTRAINT provider_hub_repository_adoption_scan_non_empty_chk CHECK (
        btrim(snapshot_key) <> ''
        AND btrim(repository_full_name) <> ''
        AND btrim(scanned_ref) <> ''
        AND btrim(head_sha) <> ''
        AND btrim(snapshot_digest) <> ''
    )
);

CREATE INDEX provider_hub_repository_adoption_scan_repository_idx
    ON provider_hub_repository_adoption_scan_snapshots (provider_slug, repository_full_name, observed_at DESC);

CREATE INDEX provider_hub_repository_adoption_scan_account_idx
    ON provider_hub_repository_adoption_scan_snapshots (external_account_id, observed_at DESC);

-- +goose Down
DROP TABLE IF EXISTS provider_hub_repository_adoption_scan_snapshots;

ALTER TABLE provider_hub_operations
    DROP CONSTRAINT provider_hub_operations_operation_type_chk,
    ADD CONSTRAINT provider_hub_operations_operation_type_chk
        CHECK (operation_type IN (
            'create_repository',
            'create_issue',
            'update_issue',
            'create_comment',
            'update_comment',
            'create_pull_request',
            'update_pull_request',
            'create_bootstrap_pull_request',
            'create_adoption_pull_request',
            'create_review_signal',
            'update_relationship'
        ));
