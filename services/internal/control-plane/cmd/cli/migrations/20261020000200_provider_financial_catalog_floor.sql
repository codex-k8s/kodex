-- +goose Up
-- Новая ещё не применявшаяся финансовая миграция следует за действующим графом.
SET ROLE control_plane_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.assign_provider_catalog_authority_version()
RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        NEW.catalog_authority_version := NEW.version;
    ELSIF (to_jsonb(NEW) - ARRAY[
          'catalog_authority_version', 'max_concurrent_executions', 'version', 'updated_at',
          'financial_observation', 'financial_received_at', 'financial_credential_revision_id',
          'financial_collector_lease_id', 'financial_collector_created_at', 'financial_sequence'])
          IS NOT DISTINCT FROM
          (to_jsonb(OLD) - ARRAY[
          'catalog_authority_version', 'max_concurrent_executions', 'version', 'updated_at',
          'financial_observation', 'financial_received_at', 'financial_credential_revision_id',
          'financial_collector_lease_id', 'financial_collector_created_at', 'financial_sequence']) THEN
        -- Display и ёмкость не назначают authority и не отзывают прежний каталог.
        NEW.catalog_authority_version := OLD.catalog_authority_version;
    ELSE
        -- Current credential, enabled, revoke и любые другие поля сохраняют gate.
        NEW.catalog_authority_version := NEW.version;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

RESET ROLE;
