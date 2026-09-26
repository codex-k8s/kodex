-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.integration_definitions
    DROP CONSTRAINT integration_definitions_adapter_check,
    ADD CONSTRAINT integration_definitions_adapter_check CHECK (adapter IN (
        'SYNTHETIC_HTTP', 'GITHUB', 'GITLAB', 'JIRA', 'CONFLUENCE',
        'EMAIL_HTTPS', 'MATTERMOST_INTERACTION', 'HTTPS_JSON_READ'
    ));

ALTER TABLE control_plane.integration_grants
    DROP CONSTRAINT integration_grants_resource_kind_check,
    ADD CONSTRAINT integration_grants_resource_kind_check CHECK (resource_kind IN (
        'SYNTHETIC_JOURNAL', 'GITHUB_REPOSITORY', 'GITLAB_PROJECT',
        'JIRA_PROJECT', 'CONFLUENCE_SPACE', 'EMAIL_SENDER',
        'MATTERMOST_CHANNEL', 'HTTPS_RESOURCE'
    ));

ALTER TABLE control_plane.integration_invocations
    DROP CONSTRAINT integration_invocations_resource_kind_check,
    ADD CONSTRAINT integration_invocations_resource_kind_check CHECK (resource_kind IN (
        'SYNTHETIC_JOURNAL', 'GITHUB_REPOSITORY', 'GITLAB_PROJECT',
        'JIRA_PROJECT', 'CONFLUENCE_SPACE', 'EMAIL_SENDER',
        'MATTERMOST_CHANNEL', 'HTTPS_RESOURCE'
    ));

RESET ROLE;
