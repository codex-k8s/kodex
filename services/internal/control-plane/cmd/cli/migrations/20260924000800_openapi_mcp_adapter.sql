-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.integration_definitions
    DROP CONSTRAINT integration_definitions_adapter_check,
    ADD CONSTRAINT integration_definitions_adapter_check CHECK (adapter IN (
        'SYNTHETIC_HTTP', 'GITHUB', 'GITLAB', 'JIRA', 'CONFLUENCE',
        'EMAIL_HTTPS', 'MATTERMOST_INTERACTION', 'HTTPS_JSON_READ', 'OPENAPI_MCP'
    ));

RESET ROLE;
