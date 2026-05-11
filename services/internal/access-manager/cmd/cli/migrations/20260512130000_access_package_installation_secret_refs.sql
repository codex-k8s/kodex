-- +goose Up
CREATE TABLE access_package_installation_secret_refs (
    id uuid PRIMARY KEY,
    package_installation_id uuid NOT NULL,
    installation_scope_type text NOT NULL,
    installation_scope_id text NOT NULL,
    logical_key text NOT NULL,
    secret_binding_ref_id uuid NOT NULL REFERENCES access_secret_binding_refs(id),
    status text NOT NULL,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    version bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (package_installation_id, logical_key),
    CONSTRAINT access_package_installation_secret_refs_scope_check CHECK (
        installation_scope_type IN ('platform', 'organization', 'project', 'repository')
        AND installation_scope_id <> ''
    ),
    CONSTRAINT access_package_installation_secret_refs_logical_key_check CHECK (logical_key <> ''),
    CONSTRAINT access_package_installation_secret_refs_status_check CHECK (status IN ('configured', 'invalid', 'disabled')),
    CONSTRAINT access_package_installation_secret_refs_metadata_check CHECK (
        jsonb_typeof(metadata) = 'object'
        AND NOT jsonb_path_exists(metadata, '$.keyvalue() ? (@.key == "" || @.key like_regex "(token|secret|password|credential|value|key)" flag "i" || @.value.type() != "string")')
    )
);

CREATE INDEX access_package_installation_secret_refs_scope_idx
    ON access_package_installation_secret_refs (installation_scope_type, installation_scope_id, status);

-- +goose Down
DROP TABLE IF EXISTS access_package_installation_secret_refs;
