-- +goose Up
SET ROLE control_plane_owner;
CREATE TABLE control_plane.session_model_catalog_bindings (
    session_id uuid PRIMARY KEY REFERENCES control_plane.sessions(id),
    organization_id uuid NOT NULL REFERENCES control_plane.organizations(id),
    provider_account_id uuid NOT NULL REFERENCES control_plane.provider_accounts(id),
    provider_account_policy_id uuid NOT NULL REFERENCES control_plane.provider_account_policy_versions(id),
    catalog_revision text NOT NULL CHECK (catalog_revision ~ '^mcat_[a-f0-9]{64}$'),
    catalog_digest text NOT NULL CHECK (catalog_digest ~ '^[a-f0-9]{64}$' AND catalog_revision = 'mcat_' || catalog_digest),
    models jsonb NOT NULL CHECK (jsonb_typeof(models) = 'array' AND jsonb_array_length(models) <= 128 AND octet_length(models::text) <= 131072),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER protect_session_model_catalog_binding BEFORE UPDATE OR DELETE ON control_plane.session_model_catalog_bindings
    FOR EACH ROW EXECUTE FUNCTION control_plane.protect_provider_model_catalog_observation();
RESET ROLE;

-- +goose Down
SET ROLE control_plane_owner;
DROP TABLE control_plane.session_model_catalog_bindings;
RESET ROLE;
