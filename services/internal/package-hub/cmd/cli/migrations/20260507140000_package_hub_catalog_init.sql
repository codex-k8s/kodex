-- +goose Up
CREATE TABLE package_hub_package_sources (
    id uuid PRIMARY KEY,
    organization_id uuid,
    slug text NOT NULL,
    display_name text NOT NULL,
    source_kind text NOT NULL,
    repository_ref text NOT NULL DEFAULT '',
    catalog_endpoint_ref text NOT NULL DEFAULT '',
    status text NOT NULL,
    last_sync_at timestamptz,
    last_error text NOT NULL DEFAULT '',
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT package_hub_package_sources_slug_chk CHECK (slug <> ''),
    CONSTRAINT package_hub_package_sources_display_name_chk CHECK (display_name <> ''),
    CONSTRAINT package_hub_package_sources_kind_chk
        CHECK (source_kind IN ('built_in', 'store_package', 'custom_repository', 'proxy')),
    CONSTRAINT package_hub_package_sources_status_chk
        CHECK (status IN ('active', 'disabled', 'blocked', 'sync_failed')),
    CONSTRAINT package_hub_package_sources_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX package_hub_package_sources_scope_slug_uidx
    ON package_hub_package_sources (
        COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid),
        slug
    );

CREATE INDEX package_hub_package_sources_scope_status_kind_idx
    ON package_hub_package_sources (organization_id, status, source_kind, slug);

CREATE TABLE package_hub_packages (
    id uuid PRIMARY KEY,
    source_id uuid REFERENCES package_hub_package_sources(id),
    slug text NOT NULL,
    package_kind text NOT NULL,
    publisher_ref text NOT NULL DEFAULT '',
    display_name jsonb NOT NULL DEFAULT '[]'::jsonb,
    description jsonb NOT NULL DEFAULT '[]'::jsonb,
    icon_object_uri text NOT NULL DEFAULT '',
    commercial_status text NOT NULL,
    trust_status text NOT NULL,
    status text NOT NULL,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT package_hub_packages_slug_chk CHECK (slug <> ''),
    CONSTRAINT package_hub_packages_kind_chk
        CHECK (package_kind IN ('plugin', 'guidance', 'store', 'platform_content')),
    CONSTRAINT package_hub_packages_commercial_status_chk
        CHECK (commercial_status IN ('free', 'paid', 'restricted', 'unknown')),
    CONSTRAINT package_hub_packages_trust_status_chk
        CHECK (trust_status IN ('built_in', 'verified', 'unverified', 'blocked')),
    CONSTRAINT package_hub_packages_status_chk
        CHECK (status IN ('available', 'hidden', 'revoked', 'blocked')),
    CONSTRAINT package_hub_packages_display_name_chk CHECK (jsonb_typeof(display_name) = 'array'),
    CONSTRAINT package_hub_packages_description_chk CHECK (jsonb_typeof(description) = 'array'),
    CONSTRAINT package_hub_packages_version_chk CHECK (version > 0)
);

CREATE UNIQUE INDEX package_hub_packages_source_slug_uidx
    ON package_hub_packages (
        COALESCE(source_id, '00000000-0000-0000-0000-000000000000'::uuid),
        slug
    );

CREATE INDEX package_hub_packages_source_kind_status_idx
    ON package_hub_packages (source_id, package_kind, status, slug);

CREATE INDEX package_hub_packages_status_trust_idx
    ON package_hub_packages (status, trust_status, commercial_status);

CREATE TABLE package_hub_package_versions (
    id uuid PRIMARY KEY,
    package_id uuid NOT NULL REFERENCES package_hub_packages(id),
    version_label text NOT NULL,
    source_ref_kind text NOT NULL,
    source_ref text NOT NULL,
    source_commit_sha text NOT NULL DEFAULT '',
    manifest_digest text NOT NULL,
    verification_status text NOT NULL,
    release_status text NOT NULL,
    revision bigint NOT NULL,
    published_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (package_id, version_label),
    CONSTRAINT package_hub_package_versions_label_chk CHECK (version_label <> ''),
    CONSTRAINT package_hub_package_versions_source_ref_kind_chk
        CHECK (source_ref_kind IN ('git_tag', 'git_commit', 'gitlink', 'proxy_ref')),
    CONSTRAINT package_hub_package_versions_source_ref_chk CHECK (source_ref <> ''),
    CONSTRAINT package_hub_package_versions_manifest_digest_chk CHECK (manifest_digest <> ''),
    CONSTRAINT package_hub_package_versions_verification_status_chk
        CHECK (verification_status IN ('verified', 'unverified', 'rejected', 'revoked')),
    CONSTRAINT package_hub_package_versions_release_status_chk
        CHECK (release_status IN ('active', 'deprecated', 'revoked', 'blocked')),
    CONSTRAINT package_hub_package_versions_revision_chk CHECK (revision > 0)
);

CREATE INDEX package_hub_package_versions_package_status_idx
    ON package_hub_package_versions (package_id, verification_status, release_status, version_label);

CREATE INDEX package_hub_package_versions_release_status_idx
    ON package_hub_package_versions (verification_status, release_status);

CREATE TABLE package_hub_manifest_snapshots (
    id uuid PRIMARY KEY,
    package_version_id uuid NOT NULL REFERENCES package_hub_package_versions(id),
    schema_version integer NOT NULL,
    payload jsonb NOT NULL,
    validation_status text NOT NULL,
    validation_errors jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL,
    CONSTRAINT package_hub_manifest_snapshots_schema_version_chk CHECK (schema_version > 0),
    CONSTRAINT package_hub_manifest_snapshots_payload_chk CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT package_hub_manifest_snapshots_validation_status_chk
        CHECK (validation_status IN ('valid', 'invalid', 'warning')),
    CONSTRAINT package_hub_manifest_snapshots_validation_errors_chk CHECK (jsonb_typeof(validation_errors) = 'array')
);

CREATE INDEX package_hub_manifest_snapshots_version_created_idx
    ON package_hub_manifest_snapshots (package_version_id, created_at DESC);

CREATE TABLE package_hub_pricing_metadata (
    id uuid PRIMARY KEY,
    package_id uuid NOT NULL REFERENCES package_hub_packages(id),
    pricing_kind text NOT NULL,
    currency text NOT NULL DEFAULT '',
    price_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    version bigint NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (package_id),
    CONSTRAINT package_hub_pricing_metadata_kind_chk
        CHECK (pricing_kind IN ('free', 'paid', 'subscription', 'usage_based', 'restricted')),
    CONSTRAINT package_hub_pricing_metadata_currency_chk CHECK (currency = '' OR length(currency) = 3),
    CONSTRAINT package_hub_pricing_metadata_payload_chk CHECK (jsonb_typeof(price_payload) = 'object'),
    CONSTRAINT package_hub_pricing_metadata_version_chk CHECK (version > 0)
);

CREATE INDEX package_hub_pricing_metadata_kind_idx
    ON package_hub_pricing_metadata (pricing_kind, currency);

-- +goose Down
DROP TABLE IF EXISTS package_hub_pricing_metadata;
DROP TABLE IF EXISTS package_hub_manifest_snapshots;
DROP TABLE IF EXISTS package_hub_package_versions;
DROP TABLE IF EXISTS package_hub_packages;
DROP TABLE IF EXISTS package_hub_package_sources;
