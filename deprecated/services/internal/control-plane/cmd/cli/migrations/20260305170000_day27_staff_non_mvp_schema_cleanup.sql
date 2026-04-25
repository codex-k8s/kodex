-- +goose Up
DROP INDEX IF EXISTS idx_prompt_templates_scope_version_desc;
DROP INDEX IF EXISTS uq_prompt_templates_active_version;
DROP INDEX IF EXISTS uq_prompt_templates_scope_version;
DROP TABLE IF EXISTS prompt_templates;

DROP INDEX IF EXISTS uq_config_entries_repository_key;
DROP INDEX IF EXISTS uq_config_entries_project_key;
DROP INDEX IF EXISTS uq_config_entries_platform_key;
DROP INDEX IF EXISTS uq_config_entries_scope_key;
DROP INDEX IF EXISTS idx_config_entries_repository_id;
DROP INDEX IF EXISTS idx_config_entries_project_id;
DROP TABLE IF EXISTS config_entries;

ALTER TABLE agents
    DROP COLUMN IF EXISTS settings_version,
    DROP COLUMN IF EXISTS settings;

-- +goose Down
ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS settings_version INTEGER NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS config_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scope TEXT NOT NULL,
    kind TEXT NOT NULL,
    project_id UUID NULL REFERENCES projects(id) ON DELETE CASCADE,
    repository_id UUID NULL REFERENCES repositories(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value_plain TEXT NOT NULL DEFAULT '',
    value_encrypted BYTEA NULL,
    sync_targets TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    mutability TEXT NOT NULL DEFAULT 'startup_required',
    is_dangerous BOOLEAN NOT NULL DEFAULT FALSE,
    created_by_user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    updated_by_user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_config_entries_scope CHECK (scope IN ('platform', 'project', 'repository')),
    CONSTRAINT chk_config_entries_kind CHECK (kind IN ('secret', 'variable')),
    CONSTRAINT chk_config_entries_mutability CHECK (mutability IN ('startup_required', 'runtime_mutable')),
    CONSTRAINT chk_config_entries_scope_refs CHECK (
        (scope = 'platform' AND project_id IS NULL AND repository_id IS NULL)
        OR (scope = 'project' AND project_id IS NOT NULL AND repository_id IS NULL)
        OR (scope = 'repository' AND repository_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_config_entries_platform_key
    ON config_entries (scope, key)
    WHERE scope = 'platform';

CREATE UNIQUE INDEX IF NOT EXISTS uq_config_entries_project_key
    ON config_entries (scope, project_id, key)
    WHERE scope = 'project';

CREATE UNIQUE INDEX IF NOT EXISTS uq_config_entries_repository_key
    ON config_entries (scope, repository_id, key)
    WHERE scope = 'repository';

CREATE INDEX IF NOT EXISTS idx_config_entries_project_id
    ON config_entries (project_id);

CREATE INDEX IF NOT EXISTS idx_config_entries_repository_id
    ON config_entries (repository_id);

CREATE TABLE IF NOT EXISTS prompt_templates (
    id BIGSERIAL PRIMARY KEY,
    scope_type TEXT NOT NULL,
    scope_id UUID NULL REFERENCES projects(id) ON DELETE CASCADE,
    role_key TEXT NOT NULL,
    template_kind TEXT NOT NULL,
    locale TEXT NOT NULL DEFAULT 'en',
    body_markdown TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'global_override',
    version INTEGER NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'draft',
    checksum TEXT NOT NULL,
    change_reason TEXT NULL,
    supersedes_version INTEGER NULL,
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activated_at TIMESTAMPTZ NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_prompt_templates_scope_type CHECK (scope_type IN ('global', 'project')),
    CONSTRAINT chk_prompt_templates_template_kind CHECK (template_kind IN ('work', 'revise')),
    CONSTRAINT chk_prompt_templates_source CHECK (source IN ('project_override', 'global_override', 'repo_seed')),
    CONSTRAINT chk_prompt_templates_status CHECK (status IN ('draft', 'active', 'archived')),
    CONSTRAINT chk_prompt_templates_scope_id CHECK (
        (scope_type = 'global' AND scope_id IS NULL)
        OR (scope_type = 'project' AND scope_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_prompt_templates_scope_version
    ON prompt_templates (scope_type, COALESCE(scope_id::text, ''), role_key, template_kind, locale, version);

CREATE UNIQUE INDEX IF NOT EXISTS uq_prompt_templates_active_version
    ON prompt_templates (scope_type, COALESCE(scope_id::text, ''), role_key, template_kind, locale)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_prompt_templates_scope_version_desc
    ON prompt_templates (scope_type, COALESCE(scope_id::text, ''), role_key, template_kind, locale, version DESC);
