-- +goose Up
CREATE TABLE organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug TEXT NOT NULL UNIQUE CHECK (slug = LOWER(slug)),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE user_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    scope TEXT NOT NULL CHECK (scope IN ('global', 'organization')),
    slug TEXT NOT NULL CHECK (slug = LOWER(slug)),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (scope = 'global' AND organization_id IS NULL)
        OR (scope = 'organization' AND organization_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX user_groups_global_slug_uidx
    ON user_groups (slug)
    WHERE organization_id IS NULL;

CREATE UNIQUE INDEX user_groups_organization_slug_uidx
    ON user_groups (organization_id, slug)
    WHERE organization_id IS NOT NULL;

CREATE TABLE organization_memberships (
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('member', 'admin', 'owner')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (organization_id, user_id)
);

CREATE TABLE user_group_memberships (
    group_id UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX organization_memberships_user_id_idx
    ON organization_memberships (user_id);

CREATE INDEX user_group_memberships_user_id_idx
    ON user_group_memberships (user_id);

-- +goose Down
DROP INDEX IF EXISTS user_group_memberships_user_id_idx;
DROP INDEX IF EXISTS organization_memberships_user_id_idx;
DROP TABLE IF EXISTS user_group_memberships;
DROP TABLE IF EXISTS organization_memberships;
DROP INDEX IF EXISTS user_groups_organization_slug_uidx;
DROP INDEX IF EXISTS user_groups_global_slug_uidx;
DROP TABLE IF EXISTS user_groups;
DROP TABLE IF EXISTS organizations;
