-- +goose Up
SET ROLE control_plane_owner;

ALTER TABLE control_plane.provider_accounts
    ADD COLUMN catalog_authority_version bigint NOT NULL DEFAULT 1;
UPDATE control_plane.provider_accounts SET catalog_authority_version = version;
ALTER TABLE control_plane.provider_accounts
    ADD CONSTRAINT provider_catalog_authority_version_bounds
    CHECK (catalog_authority_version >= 1 AND catalog_authority_version <= version);

-- +goose StatementBegin
CREATE FUNCTION control_plane.assign_provider_catalog_authority_version()
RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.catalog_authority_version := NEW.version;
    ELSIF (to_jsonb(NEW) - ARRAY['catalog_authority_version', 'max_concurrent_executions', 'version', 'updated_at'])
          IS NOT DISTINCT FROM
          (to_jsonb(OLD) - ARRAY['catalog_authority_version', 'max_concurrent_executions', 'version', 'updated_at']) THEN
        -- Только параметры ёмкости не отзывают ранее проверенную модель.
        NEW.catalog_authority_version := OLD.catalog_authority_version;
    ELSE
        -- Любое другое изменение закрыто требует наблюдения новой версии.
        NEW.catalog_authority_version := NEW.version;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.assign_provider_catalog_authority_version() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.assign_provider_catalog_authority_version() TO control_plane_runtime;
CREATE TRIGGER assign_provider_catalog_authority_version
    BEFORE INSERT OR UPDATE ON control_plane.provider_accounts
    FOR EACH ROW EXECUTE FUNCTION control_plane.assign_provider_catalog_authority_version();

RESET ROLE;
