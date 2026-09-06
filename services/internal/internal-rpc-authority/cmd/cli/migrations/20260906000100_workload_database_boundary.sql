-- +goose Up

-- Baseline сохраняет опубликованные байты main. EMAIL добавляется и при
-- обновлении существующей БД, а не только при установке пустого профиля.
RESET ROLE;
CREATE ROLE ira_email_bridge_issuer_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
GRANT internal_rpc_authority_issuer TO ira_email_bridge_issuer_g1
    WITH INHERIT FALSE, SET TRUE, ADMIN FALSE;
SET ROLE internal_rpc_authority_owner;
GRANT CONNECT ON DATABASE internal_rpc_authority TO ira_email_bridge_issuer_g1;
SET ROLE internal_rpc_authority_readback_owner;

-- Только owner-migration назначает identity и поколение. Runtime не может
-- зарегистрировать LOGIN, менять его workload либо вернуть RETIRED в CURRENT.
CREATE TABLE internal_rpc_authority.authority_workload_database_identities (
    principal text PRIMARY KEY,
    workload_id text NOT NULL CHECK (workload_id ~ '^[a-z0-9][a-z0-9.-]{1,94}[a-z0-9]$'),
    capability text NOT NULL CHECK (capability IN ('ISSUER', 'VERIFIER')),
    generation bigint NOT NULL CHECK (generation BETWEEN 1 AND 9007199254740991),
    lifecycle_status text NOT NULL CHECK (lifecycle_status IN ('CURRENT', 'RETIRED')),
    registered_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    retired_at timestamptz,
    CHECK ((lifecycle_status = 'RETIRED') = (retired_at IS NOT NULL)),
    UNIQUE (workload_id, capability, generation)
);
CREATE UNIQUE INDEX authority_workload_database_identity_current
    ON internal_rpc_authority.authority_workload_database_identities (workload_id, capability)
    WHERE lifecycle_status = 'CURRENT';
ALTER TABLE internal_rpc_authority.authority_workload_database_identities ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_workload_database_identities FORCE ROW LEVEL SECURITY;
CREATE POLICY authority_workload_database_identity_owner
    ON internal_rpc_authority.authority_workload_database_identities
    TO internal_rpc_authority_readback_owner USING (true) WITH CHECK (true);

INSERT INTO internal_rpc_authority.authority_workload_database_identities
    (principal, workload_id, capability, generation, lifecycle_status)
VALUES
    ('ira_role_image_builder_issuer_g1', 'role-image-builder', 'ISSUER', 1, 'CURRENT'),
    ('ira_image_admission_issuer_g1', 'image-admission', 'ISSUER', 1, 'CURRENT'),
    ('ira_image_promotion_issuer_g1', 'image-promotion', 'ISSUER', 1, 'CURRENT'),
    ('ira_automation_scheduler_issuer_g1', 'automation-scheduler', 'ISSUER', 1, 'CURRENT'),
    ('ira_secret_broker_issuer_g1', 'secret-broker', 'ISSUER', 1, 'CURRENT'),
    ('ira_control_plane_issuer_g1', 'control-plane', 'ISSUER', 1, 'CURRENT'),
    ('ira_stt_tts_service_issuer_g1', 'stt-tts-service', 'ISSUER', 1, 'CURRENT'),
    ('ira_control_api_gateway_issuer_g1', 'control-api-gateway', 'ISSUER', 1, 'CURRENT'),
    ('ira_integration_gateway_issuer_g1', 'integration-gateway', 'ISSUER', 1, 'CURRENT'),
    ('ira_interaction_gateway_issuer_g1', 'interaction-gateway', 'ISSUER', 1, 'CURRENT'),
    ('ira_email_bridge_issuer_g1', 'email-bridge', 'ISSUER', 1, 'CURRENT'),
    ('ira_runtime_controller_issuer_g1', 'runtime-controller', 'ISSUER', 1, 'CURRENT'),
    ('ira_session_archive_issuer_g1', 'session-archive', 'ISSUER', 1, 'CURRENT'),
    ('ira_control_plane_verifier_g1', 'control-plane', 'VERIFIER', 1, 'CURRENT'),
    ('ira_secret_broker_verifier_g1', 'secret-broker', 'VERIFIER', 1, 'CURRENT'),
    ('ira_stt_tts_service_verifier_g1', 'stt-tts-service', 'VERIFIER', 1, 'CURRENT');

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.workload_database_identity_allows_work(
    p_workload_id text, p_capability text
) RETURNS boolean
LANGUAGE sql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM internal_rpc_authority.authority_workload_database_identities AS identity
        JOIN pg_catalog.pg_roles AS login ON login.rolname = identity.principal
        JOIN pg_catalog.pg_auth_members AS membership ON membership.member = login.oid
        JOIN pg_catalog.pg_roles AS capability ON capability.oid = membership.roleid
        WHERE identity.principal = session_user
          AND identity.workload_id = p_workload_id
          AND identity.capability = p_capability
          AND identity.lifecycle_status = 'CURRENT'
          AND identity.retired_at IS NULL
          AND identity.principal = 'ira_' || replace(identity.workload_id, '-', '_') ||
              '_' || lower(identity.capability) || '_g' || identity.generation::text
          AND capability.rolname = 'internal_rpc_authority_' || lower(p_capability)
          AND membership.set_option AND NOT membership.inherit_option AND NOT membership.admin_option
          AND login.rolcanlogin AND NOT login.rolsuper AND NOT login.rolbypassrls
          AND NOT login.rolcreaterole AND NOT login.rolcreatedb
          AND NOT login.rolinherit AND NOT login.rolreplication
          AND (login.rolvaliduntil IS NULL OR login.rolvaliduntil > clock_timestamp())
        FOR SHARE OF identity
    ) AND internal_rpc_authority.runtime_restore_fence_allows_work();
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.workload_database_identity_allows_work(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.workload_database_identity_allows_work(text, text)
    TO internal_rpc_authority_issuer, internal_rpc_authority_verifier;

ALTER POLICY authority_proof_reservations_issuer ON internal_rpc_authority.authority_proof_reservations
    USING (internal_rpc_authority.workload_database_identity_allows_work(caller_workload_id, 'ISSUER'))
    WITH CHECK (internal_rpc_authority.workload_database_identity_allows_work(caller_workload_id, 'ISSUER'));
ALTER POLICY authority_proof_watermarks_issuer ON internal_rpc_authority.authority_proof_watermarks
    USING (internal_rpc_authority.workload_database_identity_allows_work(caller_workload_id, 'ISSUER'))
    WITH CHECK (internal_rpc_authority.workload_database_identity_allows_work(caller_workload_id, 'ISSUER'));
ALTER POLICY authority_replay_reservations_verifier ON internal_rpc_authority.authority_replay_reservations
    USING (internal_rpc_authority.workload_database_identity_allows_work(target_workload_id, 'VERIFIER'))
    WITH CHECK (internal_rpc_authority.workload_database_identity_allows_work(target_workload_id, 'VERIFIER'));
ALTER POLICY authority_replay_reservations_issuer_read ON internal_rpc_authority.authority_replay_reservations
    USING (internal_rpc_authority.workload_database_identity_allows_work(target_workload_id, 'ISSUER'));
ALTER POLICY authority_snapshot_watermarks_runtime ON internal_rpc_authority.authority_snapshot_watermarks
    USING (internal_rpc_authority.workload_database_identity_allows_work(target_workload_id, 'ISSUER')
        OR internal_rpc_authority.workload_database_identity_allows_work(target_workload_id, 'VERIFIER'))
    WITH CHECK (internal_rpc_authority.workload_database_identity_allows_work(target_workload_id, 'ISSUER')
        OR internal_rpc_authority.workload_database_identity_allows_work(target_workload_id, 'VERIFIER'));

-- RLS защищает владельца строки. Trigger дополнительно закрывает прямой SQL
-- обход monotonic/attestation и удаления ещё действующего replay receipt.
-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.guard_snapshot_watermark() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'authority snapshot deletion rejected' USING ERRCODE = '42501';
    END IF;
    IF NOT (internal_rpc_authority.workload_database_identity_allows_work(NEW.target_workload_id, 'ISSUER')
        OR internal_rpc_authority.workload_database_identity_allows_work(NEW.target_workload_id, 'VERIFIER'))
       OR NOT internal_rpc_authority.validate_snapshot_attestation_receipt(
           NEW.readback_attestation_receipt_id, NEW.target_workload_id,
           NEW.source_revision, NEW.source_digest_sha256)
       OR NOT EXISTS (
           SELECT 1 FROM internal_rpc_authority.authority_snapshot_history AS history
           WHERE history.source_revision = NEW.source_revision
             AND history.source_digest_sha256 = NEW.source_digest_sha256
             AND history.key_set_revision = NEW.key_set_revision
             AND history.policy_revision = NEW.policy_revision
             AND history.signer_generation = NEW.signer_generation
       ) THEN
        RAISE EXCEPTION 'authority snapshot identity or attestation rejected' USING ERRCODE = '42501';
    END IF;
    IF TG_OP = 'UPDATE' AND (
        NEW.target_workload_id <> OLD.target_workload_id
        OR NEW.source_revision < OLD.source_revision
        OR NEW.key_set_revision < OLD.key_set_revision
        OR NEW.policy_revision < OLD.policy_revision
        OR NEW.signer_generation < OLD.signer_generation
        OR (NEW.source_revision = OLD.source_revision AND NEW.source_digest_sha256 <> OLD.source_digest_sha256)
    ) THEN
        RAISE EXCEPTION 'authority snapshot rollback rejected' USING ERRCODE = '42501';
    END IF;
    NEW.served_at := clock_timestamp();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.guard_snapshot_watermark() FROM PUBLIC;
CREATE TRIGGER guard_snapshot_watermark BEFORE INSERT OR UPDATE OR DELETE
    ON internal_rpc_authority.authority_snapshot_watermarks
    FOR EACH ROW EXECUTE FUNCTION internal_rpc_authority.guard_snapshot_watermark();

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.guard_proof_watermark() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'authority proof watermark deletion rejected' USING ERRCODE = '42501';
    END IF;
    IF NOT internal_rpc_authority.workload_database_identity_allows_work(NEW.caller_workload_id, 'ISSUER') THEN
        RAISE EXCEPTION 'authority proof identity rejected' USING ERRCODE = '42501';
    END IF;
    IF TG_OP = 'UPDATE' AND (
        (NEW.caller_workload_id, NEW.operation_id, NEW.authority_proof_issuer) <>
            (OLD.caller_workload_id, OLD.operation_id, OLD.authority_proof_issuer)
        OR NEW.proof_revision < OLD.proof_revision
        OR (NEW.proof_revision = OLD.proof_revision AND
            NEW.canonical_payload_digest_sha256 <> OLD.canonical_payload_digest_sha256)
    ) THEN
        RAISE EXCEPTION 'authority proof watermark rollback rejected' USING ERRCODE = '42501';
    END IF;
    IF TG_OP = 'INSERT' OR NEW.proof_revision > OLD.proof_revision THEN
        NEW.updated_at := clock_timestamp();
    ELSE
        NEW.updated_at := OLD.updated_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.guard_proof_watermark() FROM PUBLIC;
CREATE TRIGGER guard_proof_watermark BEFORE INSERT OR UPDATE OR DELETE
    ON internal_rpc_authority.authority_proof_watermarks
    FOR EACH ROW EXECUTE FUNCTION internal_rpc_authority.guard_proof_watermark();

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.guard_runtime_reservation() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
DECLARE
    workload text;
    capability text;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'authority reservation mutation rejected' USING ERRCODE = '42501';
    END IF;
    IF TG_TABLE_NAME = 'authority_proof_reservations' THEN
        workload := CASE WHEN TG_OP = 'DELETE' THEN OLD.caller_workload_id ELSE NEW.caller_workload_id END;
        capability := 'ISSUER';
    ELSIF TG_TABLE_NAME = 'authority_replay_reservations' THEN
        workload := CASE WHEN TG_OP = 'DELETE' THEN OLD.target_workload_id ELSE NEW.target_workload_id END;
        capability := 'VERIFIER';
    ELSE
        RAISE EXCEPTION 'authority reservation table rejected' USING ERRCODE = '42501';
    END IF;
    IF NOT internal_rpc_authority.workload_database_identity_allows_work(workload, capability) THEN
        RAISE EXCEPTION 'authority reservation identity rejected' USING ERRCODE = '42501';
    END IF;
    IF TG_OP = 'DELETE' THEN
        IF OLD.expires_at >= clock_timestamp() - interval '10 minutes'
           OR OLD.accepted_at >= clock_timestamp() - interval '10 minutes' THEN
            RAISE EXCEPTION 'authority reservation retention rejected' USING ERRCODE = '42501';
        END IF;
        RETURN OLD;
    END IF;
    IF NEW.expires_at <= clock_timestamp() THEN
        RAISE EXCEPTION 'authority reservation expiry rejected' USING ERRCODE = '42501';
    END IF;
    NEW.accepted_at := clock_timestamp();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.guard_runtime_reservation() FROM PUBLIC;
CREATE TRIGGER guard_proof_reservation BEFORE INSERT OR UPDATE OR DELETE
    ON internal_rpc_authority.authority_proof_reservations
    FOR EACH ROW EXECUTE FUNCTION internal_rpc_authority.guard_runtime_reservation();
CREATE TRIGGER guard_replay_reservation BEFORE INSERT OR UPDATE OR DELETE
    ON internal_rpc_authority.authority_replay_reservations
    FOR EACH ROW EXECUTE FUNCTION internal_rpc_authority.guard_runtime_reservation();

-- +goose StatementBegin
CREATE FUNCTION internal_rpc_authority.guard_workload_database_identity() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $$
BEGIN
    IF TG_OP = 'DELETE' OR OLD.lifecycle_status = 'RETIRED' THEN
        RAISE EXCEPTION 'authority database identity retirement is irreversible' USING ERRCODE = '42501';
    END IF;
    IF (NEW.principal, NEW.workload_id, NEW.capability, NEW.generation, NEW.registered_at) <>
       (OLD.principal, OLD.workload_id, OLD.capability, OLD.generation, OLD.registered_at)
       OR NEW.lifecycle_status <> 'RETIRED' THEN
        RAISE EXCEPTION 'authority database identity mutation rejected' USING ERRCODE = '42501';
    END IF;
    NEW.retired_at := clock_timestamp();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION internal_rpc_authority.guard_workload_database_identity() FROM PUBLIC;
CREATE TRIGGER guard_workload_database_identity BEFORE UPDATE OR DELETE
    ON internal_rpc_authority.authority_workload_database_identities
    FOR EACH ROW EXECUTE FUNCTION internal_rpc_authority.guard_workload_database_identity();

RESET ROLE;
