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
            'create_review_signal',
            'update_relationship'
        ));

ALTER TABLE provider_hub_operations
    ADD COLUMN provider_object_id text NOT NULL DEFAULT '',
    ADD COLUMN repository_full_name text NOT NULL DEFAULT '',
    ADD COLUMN base_branch text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE provider_hub_operations
    DROP COLUMN base_branch,
    DROP COLUMN repository_full_name,
    DROP COLUMN provider_object_id;

ALTER TABLE provider_hub_operations
    DROP CONSTRAINT provider_hub_operations_operation_type_chk,
    ADD CONSTRAINT provider_hub_operations_operation_type_chk
        CHECK (operation_type IN (
            'create_issue',
            'update_issue',
            'create_comment',
            'update_comment',
            'create_pull_request',
            'update_pull_request',
            'create_bootstrap_pull_request',
            'create_review_signal',
            'update_relationship'
        ));
