-- +goose Up
ALTER TABLE provider_hub_operations
    DROP CONSTRAINT provider_hub_operations_operation_type_chk,
    ADD CONSTRAINT provider_hub_operations_operation_type_chk
        CHECK (operation_type IN ('create_issue', 'update_issue', 'create_comment', 'update_comment', 'create_pull_request', 'update_pull_request', 'create_review_signal', 'update_relationship'));

-- +goose Down
ALTER TABLE provider_hub_operations
    DROP CONSTRAINT provider_hub_operations_operation_type_chk,
    ADD CONSTRAINT provider_hub_operations_operation_type_chk
        CHECK (operation_type IN ('create_issue', 'update_issue', 'create_comment', 'update_comment', 'create_pull_request', 'create_review_signal', 'update_relationship'));
