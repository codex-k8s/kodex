-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL search_path=pg_catalog;
SET LOCAL lock_timeout='8s';

ALTER TABLE control_plane.provider_accounts
    ADD COLUMN personal_owner_subject_id uuid REFERENCES control_plane.subjects(id),
    DROP CONSTRAINT provider_accounts_organization_id_name_key,
    ADD CONSTRAINT provider_accounts_owner_name_key
        UNIQUE NULLS NOT DISTINCT (organization_id,personal_owner_subject_id,name),
    ADD CONSTRAINT provider_accounts_personal_creator_check
        CHECK (personal_owner_subject_id IS NULL OR personal_owner_subject_id=created_by);

CREATE INDEX provider_accounts_personal_subject_idx
    ON control_plane.provider_accounts (organization_id,personal_owner_subject_id,definition_key)
    WHERE personal_owner_subject_id IS NOT NULL;

ALTER TABLE control_plane.provider_account_policy_versions
    DROP CONSTRAINT provider_account_policy_versions_mode_check,
    DROP CONSTRAINT provider_account_policy_versions_account_candidates_check,
    ADD CONSTRAINT provider_account_policy_versions_mode_check
        CHECK (mode IN ('FIXED','LEAST_USED','WEIGHTED','PERSONAL')),
    ADD CONSTRAINT provider_account_policy_versions_account_candidates_check
        CHECK (jsonb_typeof(account_candidates)='array' AND
            (mode='PERSONAL' AND jsonb_array_length(account_candidates)=0 OR
             mode<>'PERSONAL' AND jsonb_array_length(account_candidates) BETWEEN 1 AND 128 OR
             mode='LEAST_USED' AND version_number=1 AND account_candidates='[]'::jsonb));

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_personal_model_account_owner() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $function$
BEGIN
    IF NEW.personal_owner_subject_id IS DISTINCT FROM OLD.personal_owner_subject_id THEN
        RAISE EXCEPTION 'personal model account owner is immutable';
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd

CREATE TRIGGER protect_personal_model_account_owner
    BEFORE UPDATE OF personal_owner_subject_id ON control_plane.provider_accounts
    FOR EACH ROW EXECUTE FUNCTION control_plane.protect_personal_model_account_owner();

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_shared_model_policy_accounts() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $function$
BEGIN
    IF EXISTS (
        SELECT 1 FROM jsonb_array_elements(NEW.account_candidates) candidate
        JOIN control_plane.provider_accounts account ON account.ref=candidate->>'accountRef'
        WHERE account.organization_id=NEW.organization_id AND account.personal_owner_subject_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'a personal model account cannot be assigned to a shared model policy';
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd

CREATE TRIGGER protect_shared_model_policy_accounts
    BEFORE INSERT ON control_plane.provider_account_policy_versions
    FOR EACH ROW EXECUTE FUNCTION control_plane.protect_shared_model_policy_accounts();

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_personal_model_session_account() RETURNS trigger
LANGUAGE plpgsql SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $function$
BEGIN
    IF EXISTS (SELECT 1 FROM control_plane.provider_accounts account
        WHERE account.id=NEW.provider_account_id AND account.personal_owner_subject_id IS NOT NULL
          AND (account.organization_id<>NEW.organization_id OR account.personal_owner_subject_id IS DISTINCT FROM NEW.created_by)
    ) THEN
        RAISE EXCEPTION 'personal model account requires the initiating subject';
    END IF;
    RETURN NEW;
END
$function$;
-- +goose StatementEnd

CREATE TRIGGER protect_personal_model_session_account
    BEFORE INSERT OR UPDATE OF provider_account_id,created_by,organization_id ON control_plane.sessions
    FOR EACH ROW EXECUTE FUNCTION control_plane.protect_personal_model_session_account();

REVOKE ALL ON FUNCTION control_plane.protect_personal_model_account_owner() FROM PUBLIC;
REVOKE ALL ON FUNCTION control_plane.protect_shared_model_policy_accounts() FROM PUBLIC;
REVOKE ALL ON FUNCTION control_plane.protect_personal_model_session_account() FROM PUBLIC;

RESET search_path;
RESET ROLE;
