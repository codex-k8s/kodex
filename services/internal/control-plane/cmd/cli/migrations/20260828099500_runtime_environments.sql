-- +goose Up
SET ROLE control_plane_owner;

CREATE TABLE control_plane.provider_account_policy_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^ppol_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    agent_id uuid NOT NULL REFERENCES control_plane.agents(id),
    version_number bigint NOT NULL CHECK (version_number > 0),
    mode text NOT NULL CHECK (mode IN ('FIXED', 'LEAST_USED', 'WEIGHTED')),
    account_candidates jsonb NOT NULL CHECK (jsonb_typeof(account_candidates) = 'array' AND jsonb_array_length(account_candidates) BETWEEN 1 AND 128),
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    created_by uuid REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (agent_id, version_number)
);

CREATE TRIGGER protect_provider_account_policy_version
BEFORE UPDATE OR DELETE ON control_plane.provider_account_policy_versions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

CREATE TABLE control_plane.agent_runtime_config_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^rconf_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    agent_id uuid NOT NULL REFERENCES control_plane.agents(id),
    version_number bigint NOT NULL CHECK (version_number > 0),
    provider_account_policy_id uuid NOT NULL REFERENCES control_plane.provider_account_policy_versions(id),
    runtime_profile_key text NOT NULL REFERENCES control_plane.runtime_profiles(stable_key),
    provider text NOT NULL CHECK (char_length(provider) BETWEEN 1 AND 64),
    model text NOT NULL CHECK (char_length(model) BETWEEN 1 AND 128),
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    created_by uuid REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (agent_id, version_number)
);

CREATE TRIGGER protect_agent_runtime_config_version
BEFORE UPDATE OR DELETE ON control_plane.agent_runtime_config_versions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

CREATE TABLE control_plane.agent_config_overlay_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^cov_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    agent_id uuid NOT NULL REFERENCES control_plane.agents(id),
    version_number bigint NOT NULL CHECK (version_number > 0),
    parent_version_id uuid REFERENCES control_plane.agent_config_overlay_versions(id),
    state text NOT NULL CHECK (state IN ('DRAFT', 'VALID', 'INVALID', 'PUBLISHED', 'SUPERSEDED')),
    content text NOT NULL CHECK (octet_length(content) <= 65536),
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    validation_errors jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(validation_errors) = 'array'),
    created_by uuid REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    validated_at timestamptz,
    published_at timestamptz,
    UNIQUE (agent_id, version_number)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.protect_config_overlay_content()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.ref <> NEW.ref OR OLD.organization_id <> NEW.organization_id OR
       OLD.agent_id <> NEW.agent_id OR OLD.version_number <> NEW.version_number OR
       OLD.parent_version_id IS DISTINCT FROM NEW.parent_version_id OR
       OLD.content <> NEW.content OR OLD.digest <> NEW.digest OR
       OLD.created_by IS DISTINCT FROM NEW.created_by OR OLD.created_at <> NEW.created_at OR
       (OLD.state = 'PUBLISHED' AND NEW.state <> 'SUPERSEDED') OR OLD.state = 'SUPERSEDED' THEN
        RAISE EXCEPTION 'config overlay version content is immutable';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER protect_config_overlay_content
BEFORE UPDATE ON control_plane.agent_config_overlay_versions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_config_overlay_content();

CREATE TRIGGER protect_config_overlay_delete
BEFORE DELETE ON control_plane.agent_config_overlay_versions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

CREATE UNIQUE INDEX agent_config_overlay_one_published
    ON control_plane.agent_config_overlay_versions (agent_id) WHERE state = 'PUBLISHED';

CREATE TABLE control_plane.runtime_environment_sets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^renv_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    project_id uuid REFERENCES control_plane.projects(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 160),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    state text NOT NULL DEFAULT 'ACTIVE' CHECK (state IN ('ACTIVE', 'ARCHIVED')),
    current_version_id uuid,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by uuid REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE NULLS NOT DISTINCT (organization_id, project_id, name)
);

CREATE TABLE control_plane.runtime_environment_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^renvv_[A-Za-z0-9_-]{8,88}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    environment_set_id uuid NOT NULL REFERENCES control_plane.runtime_environment_sets(id),
    version_number bigint NOT NULL CHECK (version_number > 0),
    parent_version_id uuid REFERENCES control_plane.runtime_environment_versions(id),
    non_secret_values jsonb NOT NULL CHECK (jsonb_typeof(non_secret_values) = 'array' AND jsonb_array_length(non_secret_values) <= 128),
    secret_descriptors jsonb NOT NULL CHECK (jsonb_typeof(secret_descriptors) = 'array' AND jsonb_array_length(secret_descriptors) <= 128),
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    created_by uuid REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (environment_set_id, version_number)
);

ALTER TABLE control_plane.runtime_environment_sets
    ADD CONSTRAINT runtime_environment_sets_current_version_fk
    FOREIGN KEY (current_version_id) REFERENCES control_plane.runtime_environment_versions(id);

CREATE TRIGGER protect_runtime_environment_version
BEFORE UPDATE OR DELETE ON control_plane.runtime_environment_versions
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_immutable_row();

CREATE TABLE control_plane.agent_runtime_environment_bindings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ref text NOT NULL UNIQUE CHECK (ref ~ '^aenv_[A-Za-z0-9_-]{8,89}$'),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    agent_id uuid NOT NULL UNIQUE REFERENCES control_plane.agents(id),
    environment_set_id uuid NOT NULL REFERENCES control_plane.runtime_environment_sets(id),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    digest text NOT NULL CHECK (digest ~ '^[a-f0-9]{64}$'),
    updated_by uuid REFERENCES control_plane.subjects(id),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

ALTER TABLE control_plane.agents
    ADD COLUMN current_runtime_config_id uuid,
    ADD COLUMN current_config_overlay_id uuid;

ALTER TABLE control_plane.agents
    ADD CONSTRAINT agents_current_runtime_config_fk FOREIGN KEY (current_runtime_config_id) REFERENCES control_plane.agent_runtime_config_versions(id),
    ADD CONSTRAINT agents_current_config_overlay_fk FOREIGN KEY (current_config_overlay_id) REFERENCES control_plane.agent_config_overlay_versions(id);

WITH defaults AS (
    SELECT a.id AS agent_id,
           a.organization_id,
           a.created_by,
           pa.ref AS account_ref
    FROM control_plane.agents a
    JOIN LATERAL (
        SELECT account.ref
        FROM control_plane.provider_accounts account
        WHERE account.organization_id = a.organization_id
          AND account.enabled
          AND account.state = 'AUTHORIZED'
          AND account.current_credential_revision_id IS NOT NULL
        ORDER BY account.created_at, account.ref
        LIMIT 1
    ) pa ON true
)
INSERT INTO control_plane.provider_account_policy_versions
    (ref, organization_id, agent_id, version_number, mode, account_candidates, digest, created_by)
SELECT 'ppol_' || replace(gen_random_uuid()::text, '-', ''),
       organization_id,
       agent_id,
       1,
       'FIXED',
       jsonb_build_array(jsonb_build_object('accountRef', account_ref, 'weight', 1)),
       encode(digest(convert_to('FIXED', 'UTF8') || decode('00', 'hex') ||
                     convert_to('[{"accountRef":"' || account_ref || '","weight":1}]', 'UTF8') || decode('00', 'hex'), 'sha256'), 'hex'),
       created_by
FROM defaults;

INSERT INTO control_plane.agent_runtime_config_versions
    (ref, organization_id, agent_id, version_number, provider_account_policy_id,
     runtime_profile_key, provider, model, digest, created_by)
SELECT 'rconf_' || replace(gen_random_uuid()::text, '-', ''),
       a.organization_id,
       a.id,
       1,
       policy.id,
       profile.stable_key,
       profile.provider,
       profile.model,
       encode(digest(convert_to(profile.stable_key, 'UTF8') || decode('00', 'hex') ||
                     convert_to(profile.provider, 'UTF8') || decode('00', 'hex') ||
                     convert_to(profile.model, 'UTF8') || decode('00', 'hex') ||
                     convert_to(policy.ref, 'UTF8') || decode('00', 'hex') ||
                     convert_to(policy.version_number::text, 'UTF8') || decode('00', 'hex') ||
                     convert_to(policy.digest, 'UTF8') || decode('00', 'hex'), 'sha256'), 'hex'),
       a.created_by
FROM control_plane.agents a
JOIN control_plane.runtime_profiles profile ON profile.stable_key = a.runtime_key
JOIN control_plane.provider_account_policy_versions policy ON policy.agent_id = a.id;

INSERT INTO control_plane.agent_config_overlay_versions
    (ref, organization_id, agent_id, version_number, state, content, digest, created_by, validated_at, published_at)
SELECT 'cov_' || replace(gen_random_uuid()::text, '-', ''),
       organization_id,
       id,
       1,
       'PUBLISHED',
       '',
       'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
       created_by,
       clock_timestamp(),
       clock_timestamp()
FROM control_plane.agents;

INSERT INTO control_plane.runtime_environment_sets
    (ref, organization_id, project_id, name, description, created_by)
SELECT 'renv_' || replace(gen_random_uuid()::text, '-', ''),
       grouped.organization_id,
       grouped.project_id,
       'i18n:DEFAULT_RUNTIME_ENVIRONMENT',
       'i18n:DEFAULT_RUNTIME_ENVIRONMENT_DESCRIPTION',
       grouped.created_by
FROM (
    SELECT organization_id, project_id, min(created_by::text)::uuid AS created_by
    FROM control_plane.agents
    GROUP BY organization_id, project_id
) grouped;

INSERT INTO control_plane.runtime_environment_versions
    (ref, organization_id, environment_set_id, version_number, non_secret_values, secret_descriptors, digest, created_by)
SELECT 'renvv_' || replace(gen_random_uuid()::text, '-', ''),
       environment.organization_id,
       environment.id,
       1,
       '[]'::jsonb,
       '[]'::jsonb,
       'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
       environment.created_by
FROM control_plane.runtime_environment_sets environment;

UPDATE control_plane.runtime_environment_sets environment
SET current_version_id = version.id
FROM control_plane.runtime_environment_versions version
WHERE version.environment_set_id = environment.id;

INSERT INTO control_plane.agent_runtime_environment_bindings
    (ref, organization_id, agent_id, environment_set_id, digest, updated_by)
SELECT 'aenv_' || replace(gen_random_uuid()::text, '-', ''),
       agent.organization_id,
       agent.id,
       environment.id,
       encode(digest(convert_to(agent.ref, 'UTF8') || decode('00', 'hex') ||
                     convert_to(environment.ref, 'UTF8') || decode('00', 'hex') ||
                     convert_to('1', 'UTF8') || decode('00', 'hex'), 'sha256'), 'hex'),
       agent.created_by
FROM control_plane.agents agent
JOIN control_plane.runtime_environment_sets environment
  ON environment.organization_id = agent.organization_id
 AND environment.project_id IS NOT DISTINCT FROM agent.project_id
 AND environment.name = 'i18n:DEFAULT_RUNTIME_ENVIRONMENT';

UPDATE control_plane.agents agent
SET current_runtime_config_id = config.id,
    current_config_overlay_id = overlay_version.id
FROM control_plane.agent_runtime_config_versions config,
     control_plane.agent_config_overlay_versions overlay_version
WHERE config.agent_id = agent.id
  AND overlay_version.agent_id = agent.id;

ALTER TABLE control_plane.runtime_revisions
    ADD COLUMN runtime_config_version_id uuid REFERENCES control_plane.agent_runtime_config_versions(id),
    ADD COLUMN provider_account_policy_version_id uuid REFERENCES control_plane.provider_account_policy_versions(id),
    ADD COLUMN config_overlay_version_id uuid REFERENCES control_plane.agent_config_overlay_versions(id),
    ADD COLUMN runtime_environment_version_id uuid REFERENCES control_plane.runtime_environment_versions(id),
    ADD COLUMN environment_binding_id uuid REFERENCES control_plane.agent_runtime_environment_bindings(id),
    ADD COLUMN runtime_config_ref text,
    ADD COLUMN runtime_config_version bigint,
    ADD COLUMN runtime_config_digest text,
    ADD COLUMN provider_policy_ref text,
    ADD COLUMN provider_policy_version bigint,
    ADD COLUMN provider_policy_digest text,
    ADD COLUMN config_overlay_ref text,
    ADD COLUMN config_overlay_version bigint,
    ADD COLUMN config_overlay_digest text,
    ADD COLUMN runtime_environment_ref text,
    ADD COLUMN runtime_environment_version bigint,
    ADD COLUMN runtime_environment_digest text,
    ADD COLUMN environment_binding_ref text,
    ADD COLUMN environment_binding_version bigint,
    ADD COLUMN environment_binding_digest text;

-- Существующих RuntimeRevision нет в поддерживаемом fresh-reset профиле.
ALTER TABLE control_plane.runtime_revisions
    ADD CONSTRAINT runtime_revision_runtime_config_complete CHECK (
      (runtime_config_version_id IS NULL AND runtime_config_ref IS NULL) OR
      (runtime_config_version_id IS NOT NULL AND runtime_config_ref IS NOT NULL AND runtime_config_version > 0 AND runtime_config_digest ~ '^[a-f0-9]{64}$')),
    ADD CONSTRAINT runtime_revision_provider_policy_complete CHECK (
      (provider_account_policy_version_id IS NULL AND provider_policy_ref IS NULL) OR
      (provider_account_policy_version_id IS NOT NULL AND provider_policy_ref IS NOT NULL AND provider_policy_version > 0 AND provider_policy_digest ~ '^[a-f0-9]{64}$')),
    ADD CONSTRAINT runtime_revision_overlay_complete CHECK (
      (config_overlay_version_id IS NULL AND config_overlay_ref IS NULL) OR
      (config_overlay_version_id IS NOT NULL AND config_overlay_ref IS NOT NULL AND config_overlay_version > 0 AND config_overlay_digest ~ '^[a-f0-9]{64}$')),
    ADD CONSTRAINT runtime_revision_environment_complete CHECK (
      (runtime_environment_version_id IS NULL AND runtime_environment_ref IS NULL) OR
      (runtime_environment_version_id IS NOT NULL AND runtime_environment_ref IS NOT NULL AND runtime_environment_version > 0 AND runtime_environment_digest ~ '^[a-f0-9]{64}$')),
    ADD CONSTRAINT runtime_revision_environment_binding_complete CHECK (
      (environment_binding_id IS NULL AND environment_binding_ref IS NULL) OR
      (environment_binding_id IS NOT NULL AND environment_binding_ref IS NOT NULL AND environment_binding_version > 0 AND environment_binding_digest ~ '^[a-f0-9]{64}$'));

CREATE INDEX runtime_environment_sets_catalog
    ON control_plane.runtime_environment_sets (organization_id, project_id, lower(name), ref);
CREATE INDEX runtime_environment_versions_set_version
    ON control_plane.runtime_environment_versions (environment_set_id, version_number DESC);
CREATE INDEX agent_runtime_config_versions_agent_version
    ON control_plane.agent_runtime_config_versions (agent_id, version_number DESC);
CREATE INDEX provider_account_policy_versions_agent_version
    ON control_plane.provider_account_policy_versions (agent_id, version_number DESC);

RESET ROLE;

-- +goose Down

SET ROLE control_plane_owner;

ALTER TABLE control_plane.runtime_revisions
    DROP CONSTRAINT IF EXISTS runtime_revision_environment_binding_complete,
    DROP CONSTRAINT IF EXISTS runtime_revision_environment_complete,
    DROP CONSTRAINT IF EXISTS runtime_revision_overlay_complete,
    DROP CONSTRAINT IF EXISTS runtime_revision_provider_policy_complete,
    DROP CONSTRAINT IF EXISTS runtime_revision_runtime_config_complete,
    DROP COLUMN IF EXISTS environment_binding_digest,
    DROP COLUMN IF EXISTS environment_binding_version,
    DROP COLUMN IF EXISTS environment_binding_ref,
    DROP COLUMN IF EXISTS runtime_environment_digest,
    DROP COLUMN IF EXISTS runtime_environment_version,
    DROP COLUMN IF EXISTS runtime_environment_ref,
    DROP COLUMN IF EXISTS config_overlay_digest,
    DROP COLUMN IF EXISTS config_overlay_version,
    DROP COLUMN IF EXISTS config_overlay_ref,
    DROP COLUMN IF EXISTS provider_policy_digest,
    DROP COLUMN IF EXISTS provider_policy_version,
    DROP COLUMN IF EXISTS provider_policy_ref,
    DROP COLUMN IF EXISTS runtime_config_digest,
    DROP COLUMN IF EXISTS runtime_config_version,
    DROP COLUMN IF EXISTS runtime_config_ref,
    DROP COLUMN IF EXISTS environment_binding_id,
    DROP COLUMN IF EXISTS runtime_environment_version_id,
    DROP COLUMN IF EXISTS config_overlay_version_id,
    DROP COLUMN IF EXISTS provider_account_policy_version_id,
    DROP COLUMN IF EXISTS runtime_config_version_id;

ALTER TABLE control_plane.agents
    DROP CONSTRAINT IF EXISTS agents_current_config_overlay_fk,
    DROP CONSTRAINT IF EXISTS agents_current_runtime_config_fk,
    DROP COLUMN IF EXISTS current_config_overlay_id,
    DROP COLUMN IF EXISTS current_runtime_config_id;

DROP TABLE control_plane.agent_runtime_environment_bindings;
ALTER TABLE control_plane.runtime_environment_sets DROP CONSTRAINT runtime_environment_sets_current_version_fk;
DROP TRIGGER protect_runtime_environment_version ON control_plane.runtime_environment_versions;
DROP TABLE control_plane.runtime_environment_versions;
DROP TABLE control_plane.runtime_environment_sets;
DROP INDEX control_plane.agent_config_overlay_one_published;
DROP TRIGGER protect_config_overlay_delete ON control_plane.agent_config_overlay_versions;
DROP TRIGGER protect_config_overlay_content ON control_plane.agent_config_overlay_versions;
DROP TABLE control_plane.agent_config_overlay_versions;
DROP FUNCTION control_plane.protect_config_overlay_content();
DROP TRIGGER protect_agent_runtime_config_version ON control_plane.agent_runtime_config_versions;
DROP TABLE control_plane.agent_runtime_config_versions;
DROP TRIGGER protect_provider_account_policy_version ON control_plane.provider_account_policy_versions;
DROP TABLE control_plane.provider_account_policy_versions;

RESET ROLE;
