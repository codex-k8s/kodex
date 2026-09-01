-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.integration_definitions
    ADD COLUMN schema_version text NOT NULL DEFAULT 'integrations.kodex.io/v1',
    ADD COLUMN definition_version text NOT NULL DEFAULT '1.0.0',
    ADD COLUMN origin text NOT NULL DEFAULT 'SHIPPED' CHECK (origin = 'SHIPPED'),
    ADD COLUMN digest text NOT NULL DEFAULT repeat('0', 64) CHECK (digest ~ '^[a-f0-9]{64}$'),
    ADD COLUMN adapter text NOT NULL CHECK (adapter IN (
        'SYNTHETIC_HTTP', 'GITHUB', 'GITLAB', 'JIRA', 'CONFLUENCE', 'EMAIL_HTTPS', 'MATTERMOST_INTERACTION'
    )),
    ADD COLUMN credential_secret_key text;

ALTER TABLE control_plane.integration_connections
    ALTER COLUMN credential_materialization_ref DROP NOT NULL,
    ADD COLUMN definition_version text NOT NULL DEFAULT '1.0.0',
    ADD COLUMN definition_digest text NOT NULL DEFAULT repeat('0', 64) CHECK (definition_digest ~ '^[a-f0-9]{64}$');

CREATE TABLE control_plane.integration_credential_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    connection_id uuid NOT NULL REFERENCES control_plane.integration_connections(id),
    revision bigint NOT NULL CHECK (revision > 0),
    secret_ref text NOT NULL CHECK (secret_ref ~ '^kodex-system/[a-z0-9]([-a-z0-9]*[a-z0-9])?#[A-Za-z0-9]([-._A-Za-z0-9]*[A-Za-z0-9])?$'),
    secret_uid uuid NOT NULL,
    secret_resource_version text NOT NULL CHECK (char_length(secret_resource_version) BETWEEN 1 AND 128),
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[a-f0-9]{64}$'),
    created_by uuid NOT NULL REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (connection_id, revision),
    UNIQUE (connection_id, secret_uid, secret_resource_version, content_sha256)
);

ALTER TABLE control_plane.integration_connections
    ADD COLUMN credential_revision_id uuid REFERENCES control_plane.integration_credential_revisions(id);

ALTER TABLE control_plane.integration_grants
    ADD COLUMN risk text NOT NULL DEFAULT 'READ' CHECK (risk IN ('READ', 'WRITE', 'SENSITIVE', 'DESTRUCTIVE')),
    ADD COLUMN resource_kind text NOT NULL DEFAULT 'SYNTHETIC_JOURNAL' CHECK (resource_kind IN (
        'SYNTHETIC_JOURNAL', 'GITHUB_REPOSITORY', 'GITLAB_PROJECT', 'JIRA_PROJECT',
        'CONFLUENCE_SPACE', 'EMAIL_SENDER', 'MATTERMOST_CHANNEL'
    )),
    ADD COLUMN resource_scope jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(resource_scope) = 'object' AND octet_length(resource_scope::text) <= 4096),
    ADD COLUMN resource_scope_digest text NOT NULL DEFAULT repeat('0', 64) CHECK (resource_scope_digest ~ '^[a-f0-9]{64}$'),
    ADD COLUMN definition_version text NOT NULL DEFAULT '1.0.0',
    ADD COLUMN definition_digest text NOT NULL DEFAULT repeat('0', 64) CHECK (definition_digest ~ '^[a-f0-9]{64}$');

ALTER TABLE control_plane.integration_invocations
    DROP CONSTRAINT integration_invocations_state_check,
    ADD COLUMN definition_version text NOT NULL DEFAULT '1.0.0',
    ADD COLUMN definition_digest text NOT NULL DEFAULT repeat('0', 64) CHECK (definition_digest ~ '^[a-f0-9]{64}$'),
    ADD COLUMN risk text NOT NULL DEFAULT 'READ' CHECK (risk IN ('READ', 'WRITE', 'SENSITIVE', 'DESTRUCTIVE')),
    ADD COLUMN approval_policy text NOT NULL DEFAULT 'NONE' CHECK (approval_policy IN ('NONE', 'HUMAN_EACH_EFFECT')),
    ADD COLUMN resource_kind text NOT NULL DEFAULT 'SYNTHETIC_JOURNAL' CHECK (resource_kind IN (
        'SYNTHETIC_JOURNAL', 'GITHUB_REPOSITORY', 'GITLAB_PROJECT', 'JIRA_PROJECT',
        'CONFLUENCE_SPACE', 'EMAIL_SENDER', 'MATTERMOST_CHANNEL'
    )),
    ADD COLUMN resource_scope jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(resource_scope) = 'object' AND octet_length(resource_scope::text) <= 4096),
    ADD COLUMN resource_scope_digest text NOT NULL DEFAULT repeat('0', 64) CHECK (resource_scope_digest ~ '^[a-f0-9]{64}$'),
    ADD COLUMN effect_key text NOT NULL DEFAULT '';

ALTER TABLE control_plane.integration_invocations
    ADD CONSTRAINT integration_invocations_state_check CHECK (state IN ('WAITING_APPROVAL', 'READY', 'RUNNING', 'SUCCEEDED', 'FAILED', 'REJECTED', 'CANCELLED')),
    ADD CONSTRAINT integration_invocations_approval_check CHECK (
        (risk = 'READ' AND approval_policy = 'NONE' AND state <> 'WAITING_APPROVAL') OR
        (risk IN ('WRITE', 'SENSITIVE', 'DESTRUCTIVE') AND approval_policy = 'HUMAN_EACH_EFFECT')
    );

ALTER TABLE control_plane.owner_gates
    ADD COLUMN integration_invocation_id uuid UNIQUE REFERENCES control_plane.integration_invocations(id);

CREATE TABLE control_plane.integration_effect_receipts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^[A-Za-z0-9_-]{8,96}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    invocation_id uuid NOT NULL UNIQUE REFERENCES control_plane.integration_invocations(id),
    effect_key text NOT NULL CHECK (char_length(effect_key) BETWEEN 1 AND 128),
    input_digest text NOT NULL CHECK (input_digest ~ '^[a-f0-9]{64}$'),
    provider_effect_ref text NOT NULL CHECK (char_length(provider_effect_ref) BETWEEN 1 AND 256),
    response_digest text NOT NULL CHECK (response_digest ~ '^[a-f0-9]{64}$'),
    result_summary text NOT NULL CHECK (char_length(result_summary) <= 2000),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (effect_key, input_digest)
);

ALTER TABLE control_plane.integration_invocations
    ADD COLUMN effect_receipt_id uuid UNIQUE REFERENCES control_plane.integration_effect_receipts(id);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.reject_integration_immutable_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'immutable integration record cannot be changed';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER integration_credential_revisions_immutable
BEFORE UPDATE OR DELETE ON control_plane.integration_credential_revisions
FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();

CREATE TRIGGER integration_effect_receipts_immutable
BEFORE UPDATE OR DELETE ON control_plane.integration_effect_receipts
FOR EACH ROW EXECUTE FUNCTION control_plane.reject_integration_immutable_update();

GRANT SELECT, INSERT ON control_plane.integration_credential_revisions TO control_plane_runtime;
GRANT SELECT, INSERT ON control_plane.integration_effect_receipts TO control_plane_runtime;

RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;

DROP TRIGGER IF EXISTS integration_effect_receipts_immutable ON control_plane.integration_effect_receipts;
DROP TRIGGER IF EXISTS integration_credential_revisions_immutable ON control_plane.integration_credential_revisions;
DROP FUNCTION IF EXISTS control_plane.reject_integration_immutable_update();

ALTER TABLE control_plane.integration_invocations DROP COLUMN effect_receipt_id;
DROP TABLE control_plane.integration_effect_receipts;
ALTER TABLE control_plane.owner_gates DROP COLUMN integration_invocation_id;

ALTER TABLE control_plane.integration_invocations
    DROP CONSTRAINT integration_invocations_approval_check,
    DROP CONSTRAINT integration_invocations_state_check,
    DROP COLUMN effect_key,
    DROP COLUMN resource_scope_digest,
    DROP COLUMN resource_scope,
    DROP COLUMN resource_kind,
    DROP COLUMN approval_policy,
    DROP COLUMN risk,
    DROP COLUMN definition_digest,
    DROP COLUMN definition_version,
    ADD CONSTRAINT integration_invocations_state_check CHECK (state IN ('READY', 'RUNNING', 'SUCCEEDED', 'FAILED', 'CANCELLED'));

ALTER TABLE control_plane.integration_grants
    DROP COLUMN definition_digest,
    DROP COLUMN definition_version,
    DROP COLUMN resource_scope_digest,
    DROP COLUMN resource_scope,
    DROP COLUMN resource_kind,
    DROP COLUMN risk;

ALTER TABLE control_plane.integration_connections DROP COLUMN credential_revision_id;
DROP TABLE control_plane.integration_credential_revisions;
ALTER TABLE control_plane.integration_connections
    DROP COLUMN definition_digest,
    DROP COLUMN definition_version,
    ALTER COLUMN credential_materialization_ref SET NOT NULL;

ALTER TABLE control_plane.integration_definitions
    DROP COLUMN credential_secret_key,
    DROP COLUMN adapter,
    DROP COLUMN digest,
    DROP COLUMN origin,
    DROP COLUMN definition_version,
    DROP COLUMN schema_version;

RESET ROLE;
