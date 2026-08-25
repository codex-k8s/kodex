-- +goose Up

RESET ROLE;

CREATE ROLE internal_rpc_authority_database_credential_reconciler
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE internal_rpc_authority_issuer
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE internal_rpc_authority_publisher
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE internal_rpc_authority_readback_attestor
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE internal_rpc_authority_recovery
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE internal_rpc_authority_restore_controller
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE internal_rpc_authority_verifier
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;

CREATE ROLE ira_database_credential_reconciler
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_restore_controller_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;

CREATE ROLE ira_publisher_g1
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_publisher_g2
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_publisher_g3
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_publisher_g4
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_publisher_g5
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_readback_attestor_g1
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_readback_attestor_g2
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_readback_attestor_g3
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_readback_attestor_g4
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_readback_attestor_g5
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;

GRANT ira_publisher_g1,
      ira_publisher_g2,
      ira_publisher_g3,
      ira_publisher_g4,
      ira_publisher_g5,
      ira_readback_attestor_g1,
      ira_readback_attestor_g2,
      ira_readback_attestor_g3,
      ira_readback_attestor_g4,
      ira_readback_attestor_g5
    TO internal_rpc_authority_credential_lifecycle_definer
    WITH INHERIT FALSE, SET FALSE, ADMIN TRUE;

CREATE ROLE ira_role_image_builder_issuer_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_image_admission_issuer_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_image_promotion_issuer_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_automation_scheduler_issuer_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_control_api_gateway_issuer_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_control_plane_verifier_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_control_plane_resolver_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_integration_gateway_issuer_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_interaction_gateway_issuer_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_runtime_controller_issuer_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;

GRANT internal_rpc_authority_publisher,
      internal_rpc_authority_readback_attestor
    TO internal_rpc_authority_credential_lifecycle_definer
    WITH ADMIN OPTION, INHERIT FALSE, SET FALSE;
GRANT internal_rpc_authority_readback_owner
    TO internal_rpc_authority_owner
    WITH INHERIT TRUE, SET TRUE, ADMIN FALSE;

GRANT internal_rpc_authority_database_credential_reconciler
    TO ira_database_credential_reconciler
    WITH INHERIT FALSE, SET TRUE, ADMIN FALSE;
GRANT internal_rpc_authority_restore_controller
    TO ira_restore_controller_g1
    WITH INHERIT FALSE, SET TRUE, ADMIN FALSE;
GRANT internal_rpc_authority_issuer
    TO ira_role_image_builder_issuer_g1,
       ira_image_admission_issuer_g1,
       ira_image_promotion_issuer_g1,
       ira_automation_scheduler_issuer_g1,
       ira_control_api_gateway_issuer_g1,
       ira_integration_gateway_issuer_g1,
       ira_interaction_gateway_issuer_g1,
       ira_runtime_controller_issuer_g1
    WITH INHERIT FALSE, SET TRUE, ADMIN FALSE;
GRANT internal_rpc_authority_verifier
    TO ira_control_plane_verifier_g1
    WITH INHERIT FALSE, SET TRUE, ADMIN FALSE;

SET ROLE internal_rpc_authority_owner;
GRANT CONNECT ON DATABASE internal_rpc_authority
    TO ira_database_credential_reconciler,
       ira_restore_controller_g1,
       ira_publisher_g1,
       ira_publisher_g2,
       ira_publisher_g3,
       ira_publisher_g4,
       ira_publisher_g5,
       ira_readback_attestor_g1,
       ira_readback_attestor_g2,
       ira_readback_attestor_g3,
       ira_readback_attestor_g4,
       ira_readback_attestor_g5,
       ira_role_image_builder_issuer_g1,
       ira_image_admission_issuer_g1,
       ira_image_promotion_issuer_g1,
       ira_automation_scheduler_issuer_g1,
       ira_control_api_gateway_issuer_g1,
       ira_control_plane_verifier_g1,
       ira_control_plane_resolver_g1,
       ira_integration_gateway_issuer_g1,
       ira_interaction_gateway_issuer_g1,
       ira_runtime_controller_issuer_g1;
RESET ROLE;

SET ROLE internal_rpc_authority_owner;
CREATE SCHEMA internal_rpc_authority
    AUTHORIZATION internal_rpc_authority_readback_owner;
RESET ROLE;
SET ROLE internal_rpc_authority_readback_owner;
GRANT USAGE ON SCHEMA internal_rpc_authority
    TO internal_rpc_authority_issuer,
       internal_rpc_authority_verifier,
       internal_rpc_authority_publisher,
       internal_rpc_authority_readback_attestor,
       internal_rpc_authority_database_credential_reconciler,
       internal_rpc_authority_recovery,
       internal_rpc_authority_restore_controller,
       internal_rpc_authority_credential_lifecycle_definer;
GRANT USAGE, CREATE ON SCHEMA internal_rpc_authority
    TO internal_rpc_authority_migrator;
GRANT CREATE ON SCHEMA internal_rpc_authority
    TO internal_rpc_authority_credential_lifecycle_definer;
RESET ROLE;

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;


--
-- Name: activate_readback_trust("text", "text", bigint, "text", bigint, "text", bigint, bigint, "text", "text", timestamp with time zone); Type: FUNCTION; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

-- +goose StatementBegin
CREATE FUNCTION "internal_rpc_authority"."activate_readback_trust"("p_root_id" "text", "p_root_fingerprint_sha256" "text", "p_manifest_bundle_revision" bigint, "p_manifest_bundle_digest_sha256" "text", "p_trust_source_revision" bigint, "p_trust_set_digest_sha256" "text", "p_trust_key_set_revision" bigint, "p_signer_generation" bigint, "p_predecessor_state_digest_sha256" "text", "p_served_state_digest_sha256" "text", "p_served_at" timestamp with time zone) RETURNS boolean
    LANGUAGE "plpgsql" SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $_$
DECLARE
    current internal_rpc_authority.authority_readback_trust_watermarks%ROWTYPE;
BEGIN
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_readback_attestor',
        'MEMBER'
    )
       OR p_root_id <> 'internal-rpc-authority-readback-manifest-root-v1'
       OR p_root_fingerprint_sha256 !~ '^[a-f0-9]{64}$'
       OR p_manifest_bundle_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_trust_set_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_predecessor_state_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_served_state_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_manifest_bundle_revision < 1
       OR p_trust_source_revision < 1
       OR p_trust_key_set_revision < 1
       OR p_signer_generation < 1
    THEN
        RETURN false;
    END IF;

    SELECT *
    INTO current
    FROM internal_rpc_authority.authority_readback_trust_watermarks
    WHERE attestor_id = 'internal-rpc-authority-readback-attestor'
    FOR UPDATE;
    IF FOUND THEN
        IF p_manifest_bundle_revision < current.manifest_bundle_revision
           OR p_trust_source_revision < current.trust_source_revision
           OR p_trust_key_set_revision < current.trust_key_set_revision
           OR p_signer_generation < current.signer_generation
        THEN
            RETURN false;
        END IF;
        IF p_manifest_bundle_revision = current.manifest_bundle_revision
           AND p_trust_source_revision = current.trust_source_revision
           AND p_trust_key_set_revision = current.trust_key_set_revision
        THEN
            RETURN p_served_state_digest_sha256 =
                current.served_state_digest_sha256
               AND p_manifest_bundle_digest_sha256 =
                current.manifest_bundle_digest_sha256
               AND p_trust_set_digest_sha256 =
                current.trust_set_digest_sha256;
        END IF;
        IF p_predecessor_state_digest_sha256 <>
            current.served_state_digest_sha256
        THEN
            RETURN false;
        END IF;
    END IF;

    INSERT INTO internal_rpc_authority.authority_readback_trust_watermarks (
        attestor_id,
        root_id,
        root_fingerprint_sha256,
        manifest_bundle_revision,
        manifest_bundle_digest_sha256,
        trust_source_revision,
        trust_set_digest_sha256,
        trust_key_set_revision,
        signer_generation,
        predecessor_state_digest_sha256,
        served_state_digest_sha256,
        served_at
    )
    VALUES (
        'internal-rpc-authority-readback-attestor',
        p_root_id,
        p_root_fingerprint_sha256,
        p_manifest_bundle_revision,
        p_manifest_bundle_digest_sha256,
        p_trust_source_revision,
        p_trust_set_digest_sha256,
        p_trust_key_set_revision,
        p_signer_generation,
        p_predecessor_state_digest_sha256,
        p_served_state_digest_sha256,
        p_served_at
    )
    ON CONFLICT (attestor_id) DO UPDATE
    SET root_id = EXCLUDED.root_id,
        root_fingerprint_sha256 = EXCLUDED.root_fingerprint_sha256,
        manifest_bundle_revision = EXCLUDED.manifest_bundle_revision,
        manifest_bundle_digest_sha256 =
            EXCLUDED.manifest_bundle_digest_sha256,
        trust_source_revision = EXCLUDED.trust_source_revision,
        trust_set_digest_sha256 = EXCLUDED.trust_set_digest_sha256,
        trust_key_set_revision = EXCLUDED.trust_key_set_revision,
        signer_generation = EXCLUDED.signer_generation,
        predecessor_state_digest_sha256 =
            EXCLUDED.predecessor_state_digest_sha256,
        served_state_digest_sha256 = EXCLUDED.served_state_digest_sha256,
        served_at = EXCLUDED.served_at;
    RETURN true;
END
$_$;
-- +goose StatementEnd



--
-- Name: apply_restore_fence("text", bigint, "text", "text", timestamp with time zone); Type: FUNCTION; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

-- +goose StatementBegin
CREATE FUNCTION "internal_rpc_authority"."apply_restore_fence"("p_database_cluster_id" "text", "p_restore_epoch" bigint, "p_phase" "text", "p_evidence_digest_sha256" "text", "p_safe_window_not_before" timestamp with time zone) RETURNS boolean
    LANGUAGE "plpgsql" SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $_$
DECLARE
    applied boolean;
BEGIN
    IF p_database_cluster_id <> 'internal-rpc-authority-primary'
       OR p_restore_epoch < 1
       OR p_phase NOT IN (
           'OPEN', 'QUIESCING', 'PREPARED', 'RESTORING', 'COMPLETED'
       )
       OR p_evidence_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR (p_phase = 'COMPLETED' AND p_safe_window_not_before IS NULL)
       OR (p_phase <> 'COMPLETED' AND p_safe_window_not_before IS NOT NULL)
    THEN
        RETURN false;
    END IF;

    INSERT INTO internal_rpc_authority.authority_restore_fences (
        database_cluster_id,
        restore_epoch,
        phase,
        evidence_digest_sha256,
        safe_window_not_before,
        updated_at
    )
    VALUES (
        p_database_cluster_id,
        p_restore_epoch,
        p_phase,
        p_evidence_digest_sha256,
        p_safe_window_not_before,
        clock_timestamp()
    )
    ON CONFLICT (database_cluster_id) DO UPDATE
    SET restore_epoch = EXCLUDED.restore_epoch,
        phase = EXCLUDED.phase,
        evidence_digest_sha256 = EXCLUDED.evidence_digest_sha256,
        safe_window_not_before = EXCLUDED.safe_window_not_before,
        updated_at = EXCLUDED.updated_at
    WHERE internal_rpc_authority.authority_restore_fences.restore_epoch
              <= EXCLUDED.restore_epoch
      AND (
          internal_rpc_authority.authority_restore_fences.restore_epoch
              < EXCLUDED.restore_epoch
          OR internal_rpc_authority.authority_restore_fences.evidence_digest_sha256
              = EXCLUDED.evidence_digest_sha256
      )
      AND CASE internal_rpc_authority.authority_restore_fences.phase
          WHEN 'OPEN' THEN EXCLUDED.phase IN ('OPEN', 'QUIESCING')
          WHEN 'QUIESCING' THEN EXCLUDED.phase IN ('QUIESCING', 'PREPARED')
          WHEN 'PREPARED' THEN EXCLUDED.phase IN ('PREPARED', 'RESTORING', 'COMPLETED')
          WHEN 'RESTORING' THEN EXCLUDED.phase IN ('RESTORING', 'COMPLETED')
          WHEN 'COMPLETED' THEN EXCLUDED.phase = 'COMPLETED'
          ELSE false
      END
    RETURNING true INTO applied;

    RETURN coalesce(applied, false);
END
$_$;
-- +goose StatementEnd



--
-- Name: consume_authority_readback_attestation_challenge("uuid", "uuid", "uuid", "text", bigint, "uuid", "text"); Type: FUNCTION; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

-- +goose StatementBegin
CREATE FUNCTION "internal_rpc_authority"."consume_authority_readback_attestation_challenge"("p_challenge_id" "uuid", "p_receipt_id" "uuid", "p_evidence_jti" "uuid", "p_evidence_digest_sha256" "text", "p_verifier_generation" bigint, "p_idempotency_key" "uuid", "p_semantic_request_digest_sha256" "text") RETURNS "uuid"
    LANGUAGE "plpgsql" SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $_$
DECLARE
    existing internal_rpc_authority.authority_readback_attestation_receipts%ROWTYPE;
    challenge internal_rpc_authority.authority_readback_attestation_challenges%ROWTYPE;
    intent internal_rpc_authority.authority_readback_intents%ROWTYPE;
    challenge_peer_spiffe_id text;
    accepted_at timestamptz;
BEGIN
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_readback_attestor',
        'MEMBER'
    )
       OR p_evidence_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_semantic_request_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_verifier_generation NOT BETWEEN 1 AND 9007199254740991
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
    THEN
        RAISE EXCEPTION 'readback receipt binding rejected';
    END IF;

    SELECT peer_spiffe_id
    INTO challenge_peer_spiffe_id
    FROM internal_rpc_authority.authority_readback_attestation_challenges
    WHERE challenge_id = p_challenge_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'readback challenge replay or expiry rejected';
    END IF;

    PERFORM pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            'internal_rpc_authority.readback_receipt:' ||
                challenge_peer_spiffe_id || ':' || p_idempotency_key::text,
            0
        )
    );
    accepted_at := pg_catalog.clock_timestamp();

    SELECT *
    INTO existing
    FROM internal_rpc_authority.authority_readback_attestation_receipts
    WHERE peer_spiffe_id = challenge_peer_spiffe_id
      AND idempotency_key = p_idempotency_key;
    IF FOUND THEN
        IF existing.challenge_id <> p_challenge_id
           OR existing.evidence_jti <> p_evidence_jti
           OR existing.evidence_digest_sha256 <> p_evidence_digest_sha256
           OR existing.semantic_request_digest_sha256 <>
                p_semantic_request_digest_sha256
        THEN
            RAISE EXCEPTION 'readback receipt idempotency conflict';
        END IF;
        RETURN existing.receipt_id;
    END IF;

    SELECT *
    INTO challenge
    FROM internal_rpc_authority.authority_readback_attestation_challenges
    WHERE challenge_id = p_challenge_id
    FOR UPDATE;
    IF NOT FOUND
       OR challenge.peer_spiffe_id <> challenge_peer_spiffe_id
       OR challenge.consumed_at IS NOT NULL
       OR challenge.expires_at < accepted_at
    THEN
        RAISE EXCEPTION 'readback challenge replay or expiry rejected';
    END IF;
    SELECT *
    INTO intent
    FROM internal_rpc_authority.authority_readback_intents
    WHERE intent_id = challenge.intent_id
    FOR UPDATE;
    IF NOT FOUND
       OR intent.status <> 'PINNED'
       OR intent.expires_at < accepted_at
    THEN
        RAISE EXCEPTION 'readback intent rejected';
    END IF;

    UPDATE internal_rpc_authority.authority_readback_attestation_challenges
    SET consumed_at = accepted_at
    WHERE challenge_id = challenge.challenge_id;
    INSERT INTO internal_rpc_authority.authority_readback_attestation_receipts (
        receipt_id,
        challenge_id,
        semantic_request_digest_sha256,
        evidence_digest_sha256,
        verifier_generation,
        accepted_at,
        expires_at,
        evidence_jti,
        idempotency_key,
        peer_spiffe_id
    )
    VALUES (
        p_receipt_id,
        p_challenge_id,
        p_semantic_request_digest_sha256,
        p_evidence_digest_sha256,
        p_verifier_generation,
        accepted_at,
        accepted_at + interval '5 minutes',
        p_evidence_jti,
        p_idempotency_key,
        challenge.peer_spiffe_id
    );
    INSERT INTO internal_rpc_authority.authority_snapshot_readbacks (
        readback_id,
        workload_id,
        role,
        workload_generation,
        source_revision,
        digest_sha256,
        verified_at
    )
    VALUES (
        p_receipt_id,
        intent.workload_id,
        intent.role,
        intent.workload_generation,
        intent.source_revision,
        intent.served_state_digest_sha256,
        accepted_at
    )
    ON CONFLICT (
        workload_id,
        role,
        workload_generation,
        source_revision
    ) DO UPDATE
    SET digest_sha256 =
        internal_rpc_authority.authority_snapshot_readbacks.digest_sha256
    WHERE internal_rpc_authority.authority_snapshot_readbacks.digest_sha256 =
        EXCLUDED.digest_sha256;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'readback snapshot same-revision mutation rejected';
    END IF;
    RETURN p_receipt_id;
END
$_$;
-- +goose StatementEnd



--
-- Name: issue_authority_readback_attestation_challenge("uuid", "uuid", "uuid", "text", "text", "uuid", "text", "uuid", "text"); Type: FUNCTION; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

-- +goose StatementBegin
CREATE FUNCTION "internal_rpc_authority"."issue_authority_readback_attestation_challenge"("p_intent_id" "uuid", "p_challenge_id" "uuid", "p_challenge_jti" "uuid", "p_challenge_nonce" "text", "p_challenge_digest_sha256" "text", "p_readback_credential_jti" "uuid", "p_readback_credential_digest_sha256" "text", "p_idempotency_key" "uuid", "p_semantic_request_digest_sha256" "text") RETURNS "uuid"
    LANGUAGE "plpgsql" SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $_$
DECLARE
    accepted_id uuid;
    issued_at timestamptz;
BEGIN
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_readback_attestor',
        'MEMBER'
    )
       OR p_readback_credential_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_semantic_request_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_challenge_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR pg_catalog.octet_length(p_challenge_nonce) NOT BETWEEN 32 AND 256
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
    THEN
        RAISE EXCEPTION 'readback challenge binding rejected';
    END IF;
    issued_at := pg_catalog.clock_timestamp();

    INSERT INTO internal_rpc_authority.authority_readback_attestation_challenges (
        challenge_id,
        challenge_jti,
        intent_id,
        request_digest_sha256,
        nonce,
        issued_at,
        expires_at,
        peer_spiffe_id,
        readback_credential_jti,
        readback_credential_digest_sha256,
        idempotency_key,
        semantic_request_digest_sha256,
        challenge_digest_sha256
    )
    SELECT
        p_challenge_id,
        p_challenge_jti,
        intent.intent_id,
        p_semantic_request_digest_sha256,
        p_challenge_nonce,
        issued_at,
        issued_at + interval '30 seconds',
        intent.workload_spiffe_id,
        p_readback_credential_jti,
        p_readback_credential_digest_sha256,
        p_idempotency_key,
        p_semantic_request_digest_sha256,
        p_challenge_digest_sha256
    FROM internal_rpc_authority.authority_readback_intents AS intent
    WHERE intent.intent_id = p_intent_id
      AND intent.status = 'PINNED'
      AND intent.expires_at >= issued_at + interval '30 seconds'
    ON CONFLICT (peer_spiffe_id, idempotency_key) DO UPDATE
    SET semantic_request_digest_sha256 =
        internal_rpc_authority.authority_readback_attestation_challenges
            .semantic_request_digest_sha256
    WHERE internal_rpc_authority.authority_readback_attestation_challenges
            .semantic_request_digest_sha256 =
            EXCLUDED.semantic_request_digest_sha256
      AND internal_rpc_authority.authority_readback_attestation_challenges
            .intent_id = EXCLUDED.intent_id
      AND internal_rpc_authority.authority_readback_attestation_challenges
            .readback_credential_digest_sha256 =
            EXCLUDED.readback_credential_digest_sha256
    RETURNING challenge_id INTO accepted_id;

    RETURN accepted_id;
END
$_$;
-- +goose StatementEnd




-- +goose StatementBegin
CREATE FUNCTION "internal_rpc_authority"."publisher_append_snapshot_history"("p_source_revision" bigint, "p_source_digest_sha256" "text", "p_key_set_revision" bigint, "p_policy_revision" bigint, "p_signer_generation" bigint, "p_predecessor_revision" bigint, "p_predecessor_digest_sha256" "text", "p_snapshot_compact_jws" "text", "p_publication_intent_id" "uuid", "p_publication_input_digest_sha256" "text", "p_expected_readback_count" integer, "p_published_at" timestamp with time zone) RETURNS boolean
    LANGUAGE "plpgsql" SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $_$
DECLARE
    latest internal_rpc_authority.authority_snapshot_history%ROWTYPE;
    existing internal_rpc_authority.authority_snapshot_history%ROWTYPE;
    zero_digest constant text :=
        '0000000000000000000000000000000000000000000000000000000000000000';
BEGIN
    PERFORM pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            'internal_rpc_authority.publisher_snapshot_history',
            0
        )
    );
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_publisher',
        'MEMBER'
    )
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
       OR p_source_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_key_set_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_policy_revision NOT BETWEEN 1 AND 9007199254740991
       OR p_signer_generation NOT BETWEEN 1 AND 9007199254740991
       OR p_source_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_publication_input_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_predecessor_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR pg_catalog.octet_length(p_snapshot_compact_jws)
            NOT BETWEEN 64 AND 1048576
       OR p_expected_readback_count NOT BETWEEN 1 AND 384
       OR p_published_at IS NULL
       OR p_published_at < pg_catalog.clock_timestamp() - interval '5 minutes'
       OR p_published_at > pg_catalog.clock_timestamp() + interval '5 seconds'
    THEN
        RETURN false;
    END IF;

    SELECT *
    INTO existing
    FROM internal_rpc_authority.authority_snapshot_history
    WHERE source_revision = p_source_revision
    FOR UPDATE;
    IF FOUND THEN
        RETURN existing.source_digest_sha256 = p_source_digest_sha256
           AND existing.key_set_revision = p_key_set_revision
           AND existing.policy_revision = p_policy_revision
           AND existing.signer_generation = p_signer_generation
           AND existing.predecessor_revision = p_predecessor_revision
           AND existing.predecessor_digest_sha256 =
                p_predecessor_digest_sha256
           AND existing.snapshot_compact_jws = p_snapshot_compact_jws
           AND existing.publication_intent_id = p_publication_intent_id
           AND existing.publication_input_digest_sha256 =
                p_publication_input_digest_sha256
           AND existing.expected_readback_count = p_expected_readback_count
           AND existing.published_at = p_published_at;
    END IF;

    SELECT *
    INTO latest
    FROM internal_rpc_authority.authority_snapshot_history
    ORDER BY source_revision DESC
    LIMIT 1
    FOR UPDATE;
    IF p_source_revision = 1 THEN
        IF FOUND
           OR p_predecessor_revision <> 0
           OR p_predecessor_digest_sha256 <> zero_digest
        THEN
            RETURN false;
        END IF;
    ELSIF NOT FOUND
       OR p_source_revision <> latest.source_revision + 1
       OR p_predecessor_revision <> latest.source_revision
       OR p_predecessor_digest_sha256 <> latest.source_digest_sha256
    THEN
        RETURN false;
    END IF;

    INSERT INTO internal_rpc_authority.authority_snapshot_history (
        source_revision,
        source_digest_sha256,
        key_set_revision,
        policy_revision,
        signer_generation,
        predecessor_revision,
        predecessor_digest_sha256,
        canonical_payload,
        published_at,
        snapshot_compact_jws,
        publication_intent_id,
        publication_input_digest_sha256,
        expected_readback_count
    )
    VALUES (
        p_source_revision,
        p_source_digest_sha256,
        p_key_set_revision,
        p_policy_revision,
        p_signer_generation,
        p_predecessor_revision,
        p_predecessor_digest_sha256,
        pg_catalog.jsonb_build_object(
            'source_revision', p_source_revision,
            'source_digest_sha256', p_source_digest_sha256
        ),
        p_published_at,
        p_snapshot_compact_jws,
        p_publication_intent_id,
        p_publication_input_digest_sha256,
        p_expected_readback_count
    );
    INSERT INTO internal_rpc_authority.authority_rotation_intents (
        intent_id,
        source_revision,
        source_digest_sha256,
        status,
        created_at,
        updated_at
    )
    VALUES (
        p_publication_intent_id,
        p_source_revision,
        p_source_digest_sha256,
        'PREPARED',
        pg_catalog.clock_timestamp(),
        pg_catalog.clock_timestamp()
    );
    RETURN true;
END
$_$;
-- +goose StatementEnd



--
-- Name: publisher_promote_snapshot("uuid", bigint, "text", integer); Type: FUNCTION; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

-- +goose StatementBegin
CREATE FUNCTION "internal_rpc_authority"."publisher_promote_snapshot"("p_publication_intent_id" "uuid", "p_source_revision" bigint, "p_source_digest_sha256" "text", "p_expected_readback_count" integer) RETURNS boolean
    LANGUAGE "plpgsql" SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $_$
DECLARE
    matched integer;
    publication internal_rpc_authority.authority_snapshot_history%ROWTYPE;
BEGIN
    IF NOT pg_catalog.pg_has_role(
        session_user,
        'internal_rpc_authority_publisher',
        'MEMBER'
    )
       OR NOT internal_rpc_authority.runtime_restore_fence_allows_work()
       OR p_source_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR p_expected_readback_count NOT BETWEEN 1 AND 384
    THEN
        RETURN false;
    END IF;
    SELECT *
    INTO publication
    FROM internal_rpc_authority.authority_snapshot_history
    WHERE publication_intent_id = p_publication_intent_id
      AND source_revision = p_source_revision
      AND source_digest_sha256 = p_source_digest_sha256
      AND expected_readback_count = p_expected_readback_count
    FOR UPDATE;
    IF NOT FOUND THEN
        RETURN false;
    END IF;
    SELECT pg_catalog.count(*)::integer
    INTO matched
    FROM internal_rpc_authority.authority_snapshot_readbacks
    WHERE source_revision = p_source_revision
      AND digest_sha256 = p_source_digest_sha256;
    IF matched <> p_expected_readback_count THEN
        RETURN false;
    END IF;
    UPDATE internal_rpc_authority.authority_rotation_intents
    SET status = 'PROMOTED',
        updated_at = pg_catalog.clock_timestamp()
    WHERE intent_id = p_publication_intent_id
      AND source_revision = p_source_revision
      AND source_digest_sha256 = p_source_digest_sha256
      AND status IN ('PREPARED', 'DELIVERED', 'PROMOTED');
    RETURN FOUND;
END
$_$;
-- +goose StatementEnd



--
-- Name: reconcile_runtime_database_identity("text", "text", bigint, "text", "uuid", "text"); Type: FUNCTION; Schema: internal_rpc_authority; Owner: internal_rpc_authority_credential_lifecycle_definer
--

-- +goose StatementBegin
CREATE FUNCTION "internal_rpc_authority"."reconcile_runtime_database_identity"("requested_capability" "text", "requested_principal" "text", "requested_generation" bigint, "requested_status" "text", "requested_request_id" "uuid", "requested_registered_set_digest_sha256" "text") RETURNS boolean
    LANGUAGE "plpgsql" SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $_$
DECLARE
    capability_role text;
    accepted boolean;
BEGIN
    IF NOT pg_has_role(
        session_user,
        'internal_rpc_authority_database_credential_reconciler',
        'MEMBER'
    ) THEN
        RAISE EXCEPTION 'database credential reconciler identity rejected';
    END IF;
    IF requested_status NOT IN ('CURRENT', 'NEXT', 'PREVIOUS')
       OR requested_registered_set_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR NOT (
           (
               requested_capability = 'PUBLISHER'
               AND requested_principal =
                   'ira_publisher_g' || requested_generation::text
               AND requested_generation BETWEEN 1 AND 5
           )
           OR (
               requested_capability = 'READBACK_ATTESTOR'
               AND requested_principal =
                   'ira_readback_attestor_g' || requested_generation::text
               AND requested_generation BETWEEN 1 AND 5
           )
       )
    THEN
        RAISE EXCEPTION 'database credential registry tuple rejected';
    END IF;

    capability_role := CASE requested_capability
        WHEN 'PUBLISHER' THEN 'internal_rpc_authority_publisher'
        WHEN 'READBACK_ATTESTOR' THEN 'internal_rpc_authority_readback_attestor'
    END;
    EXECUTE format('GRANT %I TO %I', capability_role, requested_principal);
    IF requested_status IN ('CURRENT', 'NEXT') THEN
        EXECUTE format('ALTER ROLE %I LOGIN', requested_principal);
    ELSE
        EXECUTE format('ALTER ROLE %I NOLOGIN', requested_principal);
    END IF;

    INSERT INTO internal_rpc_authority.database_credential_reconciliation_receipts (
        request_id, canonical_request_digest_sha256
    )
    VALUES (requested_request_id, requested_registered_set_digest_sha256)
    ON CONFLICT (request_id) DO UPDATE
    SET canonical_request_digest_sha256 =
        internal_rpc_authority.database_credential_reconciliation_receipts
            .canonical_request_digest_sha256
    WHERE internal_rpc_authority.database_credential_reconciliation_receipts
            .canonical_request_digest_sha256 =
        EXCLUDED.canonical_request_digest_sha256;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'database credential idempotency conflict';
    END IF;

    INSERT INTO internal_rpc_authority.authority_runtime_database_identities (
        capability, principal, generation, lifecycle_status,
        registered_set_digest_sha256, reconciled_at, retired_at
    )
    VALUES (
        requested_capability, requested_principal, requested_generation,
        requested_status, requested_registered_set_digest_sha256,
        clock_timestamp(), NULL
    )
    ON CONFLICT (capability, generation) DO UPDATE
    SET lifecycle_status = EXCLUDED.lifecycle_status,
        registered_set_digest_sha256 = EXCLUDED.registered_set_digest_sha256,
        reconciled_at = EXCLUDED.reconciled_at,
        retired_at = NULL
    WHERE internal_rpc_authority.authority_runtime_database_identities.principal =
            EXCLUDED.principal
      AND CASE
          internal_rpc_authority.authority_runtime_database_identities
              .lifecycle_status
          WHEN 'NEXT' THEN EXCLUDED.lifecycle_status IN (
              'NEXT', 'CURRENT', 'PREVIOUS'
          )
          WHEN 'CURRENT' THEN EXCLUDED.lifecycle_status IN (
              'CURRENT', 'PREVIOUS'
          )
          WHEN 'PREVIOUS' THEN EXCLUDED.lifecycle_status = 'PREVIOUS'
          ELSE false
      END
    RETURNING true INTO accepted;
    RETURN coalesce(accepted, false);
END
$_$;
-- +goose StatementEnd



--
-- Name: record_database_credential_session_readback("text", "uuid"); Type: FUNCTION; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

-- +goose StatementBegin
CREATE FUNCTION "internal_rpc_authority"."record_database_credential_session_readback"("p_credential_digest_sha256" "text", "p_pod_uid" "uuid") RETURNS "text"
    LANGUAGE "plpgsql" SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $_$
DECLARE
    identity internal_rpc_authority.authority_runtime_database_identities%ROWTYPE;
BEGIN
    IF p_credential_digest_sha256 !~ '^[a-f0-9]{64}$' THEN
        RAISE EXCEPTION 'database credential readback digest rejected';
    END IF;
    SELECT *
    INTO identity
    FROM internal_rpc_authority.authority_runtime_database_identities
    WHERE principal = session_user
      AND lifecycle_status IN ('CURRENT', 'NEXT');
    IF NOT FOUND
       OR NOT (
           (
               identity.capability = 'PUBLISHER'
               AND pg_catalog.pg_has_role(
                   session_user,
                   'internal_rpc_authority_publisher',
                   'MEMBER'
               )
           )
           OR (
               identity.capability = 'READBACK_ATTESTOR'
               AND pg_catalog.pg_has_role(
                   session_user,
                   'internal_rpc_authority_readback_attestor',
                   'MEMBER'
               )
           )
       )
    THEN
        RAISE EXCEPTION 'database credential session identity rejected';
    END IF;

    INSERT INTO internal_rpc_authority.database_credential_session_readbacks (
        capability,
        generation,
        lifecycle_status,
        principal,
        credential_digest_sha256,
        pod_uid,
        observed_at
    )
    VALUES (
        identity.capability,
        identity.generation,
        identity.lifecycle_status,
        session_user,
        p_credential_digest_sha256,
        p_pod_uid,
        clock_timestamp()
    )
    ON CONFLICT (capability, generation, lifecycle_status, pod_uid) DO UPDATE
    SET principal = EXCLUDED.principal,
        credential_digest_sha256 = EXCLUDED.credential_digest_sha256,
        observed_at = EXCLUDED.observed_at;
    RETURN identity.lifecycle_status;
END
$_$;
-- +goose StatementEnd



--
-- Name: retire_runtime_database_identity("text", "text", bigint, "uuid", "text"); Type: FUNCTION; Schema: internal_rpc_authority; Owner: internal_rpc_authority_credential_lifecycle_definer
--

-- +goose StatementBegin
CREATE FUNCTION "internal_rpc_authority"."retire_runtime_database_identity"("requested_capability" "text", "requested_principal" "text", "requested_generation" bigint, "requested_request_id" "uuid", "requested_registered_set_digest_sha256" "text") RETURNS boolean
    LANGUAGE "plpgsql" SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $_$
DECLARE
    capability_role text;
    accepted boolean;
BEGIN
    IF NOT pg_has_role(
        session_user,
        'internal_rpc_authority_database_credential_reconciler',
        'MEMBER'
    ) THEN
        RAISE EXCEPTION 'database credential reconciler identity rejected';
    END IF;
    IF requested_registered_set_digest_sha256 !~ '^[a-f0-9]{64}$'
       OR NOT (
           (
               requested_capability = 'PUBLISHER'
               AND requested_principal =
                   'ira_publisher_g' || requested_generation::text
               AND requested_generation BETWEEN 1 AND 5
           )
           OR (
               requested_capability = 'READBACK_ATTESTOR'
               AND requested_principal =
                   'ira_readback_attestor_g' || requested_generation::text
               AND requested_generation BETWEEN 1 AND 5
           )
       )
    THEN
        RAISE EXCEPTION 'database credential retirement tuple rejected';
    END IF;
    capability_role := CASE requested_capability
        WHEN 'PUBLISHER' THEN 'internal_rpc_authority_publisher'
        WHEN 'READBACK_ATTESTOR' THEN 'internal_rpc_authority_readback_attestor'
    END;
    EXECUTE format('ALTER ROLE %I NOLOGIN', requested_principal);
    EXECUTE format('REVOKE %I FROM %I', capability_role, requested_principal);
    PERFORM pg_terminate_backend(activity.pid)
    FROM pg_stat_activity AS activity
    WHERE activity.usename = requested_principal
      AND activity.pid <> pg_backend_pid();

    INSERT INTO internal_rpc_authority.database_credential_reconciliation_receipts (
        request_id, canonical_request_digest_sha256
    )
    VALUES (requested_request_id, requested_registered_set_digest_sha256)
    ON CONFLICT (request_id) DO UPDATE
    SET canonical_request_digest_sha256 =
        internal_rpc_authority.database_credential_reconciliation_receipts
            .canonical_request_digest_sha256
    WHERE internal_rpc_authority.database_credential_reconciliation_receipts
            .canonical_request_digest_sha256 =
        EXCLUDED.canonical_request_digest_sha256;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'database credential idempotency conflict';
    END IF;
    INSERT INTO internal_rpc_authority.authority_runtime_database_identities (
        capability, principal, generation, lifecycle_status,
        registered_set_digest_sha256, reconciled_at, retired_at
    )
    VALUES (
        requested_capability, requested_principal, requested_generation,
        'RETIRED', requested_registered_set_digest_sha256,
        clock_timestamp(), clock_timestamp()
    )
    ON CONFLICT (capability, generation) DO UPDATE
    SET lifecycle_status = 'RETIRED',
        registered_set_digest_sha256 = EXCLUDED.registered_set_digest_sha256,
        reconciled_at = EXCLUDED.reconciled_at,
        retired_at = EXCLUDED.retired_at
    WHERE internal_rpc_authority.authority_runtime_database_identities.principal =
            EXCLUDED.principal
      AND internal_rpc_authority.authority_runtime_database_identities
            .lifecycle_status IN ('CURRENT', 'NEXT', 'PREVIOUS', 'RETIRED')
    RETURNING true INTO accepted;
    RETURN coalesce(accepted, false);
END
$_$;
-- +goose StatementEnd



--
-- Name: runtime_restore_fence_allows_work(); Type: FUNCTION; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

-- +goose StatementBegin
CREATE FUNCTION "internal_rpc_authority"."runtime_restore_fence_allows_work"() RETURNS boolean
    LANGUAGE "sql" STABLE SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $$
    SELECT count(*) = 1
       AND bool_and(
           fence.phase = 'OPEN'
           OR (
               fence.phase = 'COMPLETED'
               AND fence.safe_window_not_before IS NOT NULL
               AND fence.safe_window_not_before <= clock_timestamp()
           )
       )
    FROM internal_rpc_authority.authority_restore_fences AS fence;
$$;
-- +goose StatementEnd



--
-- Name: validate_snapshot_attestation_receipt("uuid", "text", bigint, "text"); Type: FUNCTION; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

-- +goose StatementBegin
CREATE FUNCTION "internal_rpc_authority"."validate_snapshot_attestation_receipt"("p_receipt_id" "uuid", "p_workload_id" "text", "p_source_revision" bigint, "p_source_digest_sha256" "text") RETURNS boolean
    LANGUAGE "sql" STABLE SECURITY DEFINER
    SET "search_path" TO 'pg_catalog', 'internal_rpc_authority', 'pg_temp'
    AS $$
    SELECT EXISTS (
        SELECT 1
        FROM internal_rpc_authority.authority_readback_attestation_receipts
            AS receipt
        JOIN internal_rpc_authority.authority_readback_attestation_challenges
            AS challenge
          ON challenge.challenge_id = receipt.challenge_id
         AND challenge.consumed_at IS NOT NULL
        JOIN internal_rpc_authority.authority_readback_intents AS intent
          ON intent.intent_id = challenge.intent_id
        JOIN internal_rpc_authority.authority_snapshot_history AS history
          ON history.source_revision = intent.source_revision
         AND history.source_digest_sha256 =
             intent.served_state_digest_sha256
        JOIN internal_rpc_authority.authority_rotation_intents AS rotation
          ON rotation.intent_id = history.publication_intent_id
         AND rotation.source_revision = history.source_revision
         AND rotation.source_digest_sha256 =
             history.source_digest_sha256
         AND rotation.status = 'PROMOTED'
        WHERE receipt.receipt_id = p_receipt_id
          AND receipt.expires_at > pg_catalog.clock_timestamp()
          AND receipt.peer_spiffe_id = intent.workload_spiffe_id
          AND intent.kind = 'SNAPSHOT'
          AND intent.status = 'PINNED'
          AND intent.expires_at > pg_catalog.clock_timestamp()
          AND intent.workload_id = p_workload_id
          AND intent.source_revision = p_source_revision
          AND intent.served_state_digest_sha256 = p_source_digest_sha256
    );
$$;
-- +goose StatementEnd



SET default_tablespace = '';

SET default_table_access_method = "heap";

--
-- Name: authority_key_delivery_readbacks; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_key_delivery_readbacks" (
    "readback_id" "uuid" NOT NULL,
    "workload_id" "text" NOT NULL,
    "role" "text" NOT NULL,
    "workload_generation" bigint NOT NULL,
    "source_revision" bigint NOT NULL,
    "digest_sha256" "text" NOT NULL,
    "verified_at" timestamp with time zone NOT NULL,
    CONSTRAINT "authority_key_delivery_readbacks_digest_sha256_check" CHECK (("digest_sha256" ~ '^[a-f0-9]{64}$'::"text"))
);



--
-- Name: authority_proof_reservations; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_proof_reservations" (
    "caller_workload_id" "text" NOT NULL,
    "jti" "uuid" NOT NULL,
    "canonical_digest_sha256" "text" NOT NULL,
    "expires_at" timestamp with time zone NOT NULL,
    "accepted_at" timestamp with time zone DEFAULT "clock_timestamp"() NOT NULL,
    CONSTRAINT "authority_proof_reservations_canonical_digest_sha256_check" CHECK (("canonical_digest_sha256" ~ '^[a-f0-9]{64}$'::"text"))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_proof_reservations" FORCE ROW LEVEL SECURITY;



--
-- Name: authority_proof_watermarks; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_proof_watermarks" (
    "caller_workload_id" "text" NOT NULL,
    "operation_id" "text" NOT NULL,
    "authority_proof_issuer" "text" NOT NULL,
    "proof_revision" bigint NOT NULL,
    "canonical_payload_digest_sha256" "text" CONSTRAINT "authority_proof_watermarks_canonical_payload_digest_sh_not_null" NOT NULL,
    "updated_at" timestamp with time zone NOT NULL,
    CONSTRAINT "authority_proof_watermarks_canonical_payload_digest_sha25_check" CHECK (("canonical_payload_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_proof_watermarks_proof_revision_check" CHECK ((("proof_revision" >= 1) AND ("proof_revision" <= '9007199254740991'::bigint)))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_proof_watermarks" FORCE ROW LEVEL SECURITY;



--
-- Name: authority_publisher_delivery_receipts; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_publisher_delivery_receipts" (
    "idempotency_key" "uuid" NOT NULL,
    "directive_jti" "uuid" NOT NULL,
    "directive_digest_sha256" "text" CONSTRAINT "authority_publisher_delivery_r_directive_digest_sha256_not_null" NOT NULL,
    "delivery_receipt_compact_jws" "text" CONSTRAINT "authority_publisher_deliver_delivery_receipt_compact_j_not_null" NOT NULL,
    "role_credential_digest_sha256" "text" CONSTRAINT "authority_publisher_deliver_role_credential_digest_sha_not_null" NOT NULL,
    "credential_generation" bigint CONSTRAINT "authority_publisher_delivery_rec_credential_generation_not_null" NOT NULL,
    "ack_key_generation" bigint CONSTRAINT "authority_publisher_delivery_receip_ack_key_generation_not_null" NOT NULL,
    "accepted_at" timestamp with time zone NOT NULL,
    CONSTRAINT "authority_publisher_delivery_delivery_receipt_compact_jws_check" CHECK ((("octet_length"("delivery_receipt_compact_jws") >= 64) AND ("octet_length"("delivery_receipt_compact_jws") <= 8192))),
    CONSTRAINT "authority_publisher_delivery_rece_directive_digest_sha256_check" CHECK (("directive_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_publisher_delivery_receip_credential_generation_check" CHECK ((("credential_generation" >= 1) AND ("credential_generation" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_publisher_delivery_receipts_ack_key_generation_check" CHECK ((("ack_key_generation" >= 1) AND ("ack_key_generation" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_publisher_delivery_role_credential_digest_sha25_check" CHECK (("role_credential_digest_sha256" ~ '^[a-f0-9]{64}$'::"text"))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_publisher_delivery_receipts" FORCE ROW LEVEL SECURITY;



--
-- Name: authority_readback_attestation_challenges; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_readback_attestation_challenges" (
    "challenge_id" "uuid" NOT NULL,
    "challenge_jti" "uuid" CONSTRAINT "authority_readback_attestation_challenge_challenge_jti_not_null" NOT NULL,
    "intent_id" "uuid" NOT NULL,
    "request_digest_sha256" "text" CONSTRAINT "authority_readback_attestation_c_request_digest_sha256_not_null" NOT NULL,
    "nonce" "text" NOT NULL,
    "issued_at" timestamp with time zone NOT NULL,
    "expires_at" timestamp with time zone NOT NULL,
    "consumed_at" timestamp with time zone,
    "peer_spiffe_id" "text" CONSTRAINT "authority_readback_attestation_challeng_peer_spiffe_id_not_null" NOT NULL,
    "readback_credential_jti" "uuid" CONSTRAINT "authority_readback_attestation_readback_credential_jti_not_null" NOT NULL,
    "readback_credential_digest_sha256" "text" CONSTRAINT "authority_readback_attestat_readback_credential_digest_not_null" NOT NULL,
    "idempotency_key" "uuid" CONSTRAINT "authority_readback_attestation_challen_idempotency_key_not_null" NOT NULL,
    "semantic_request_digest_sha256" "text" CONSTRAINT "authority_readback_attesta_semantic_request_digest_sh_not_null1" NOT NULL,
    "challenge_digest_sha256" "text" CONSTRAINT "authority_readback_attestation_challenge_digest_sha256_not_null" NOT NULL,
    CONSTRAINT "authority_readback_attestati_readback_credential_digest_s_check" CHECK (("readback_credential_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_attestati_semantic_request_digest_sha_check1" CHECK (("semantic_request_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_attestation_ch_challenge_digest_sha256_check" CHECK (("challenge_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_attestation_chal_request_digest_sha256_check" CHECK (("request_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_attestation_challenges_peer_spiffe_id_check" CHECK (("peer_spiffe_id" ~ '^spiffe://kodex[.]local/'::"text"))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_readback_attestation_challenges" FORCE ROW LEVEL SECURITY;



--
-- Name: authority_readback_attestation_receipts; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_readback_attestation_receipts" (
    "receipt_id" "uuid" NOT NULL,
    "challenge_id" "uuid" NOT NULL,
    "semantic_request_digest_sha256" "text" CONSTRAINT "authority_readback_attestat_semantic_request_digest_sh_not_null" NOT NULL,
    "evidence_digest_sha256" "text" CONSTRAINT "authority_readback_attestation__evidence_digest_sha256_not_null" NOT NULL,
    "verifier_generation" bigint CONSTRAINT "authority_readback_attestation_rec_verifier_generation_not_null" NOT NULL,
    "accepted_at" timestamp with time zone NOT NULL,
    "expires_at" timestamp with time zone NOT NULL,
    "evidence_jti" "uuid" NOT NULL,
    "idempotency_key" "uuid" CONSTRAINT "authority_readback_attestation_receipt_idempotency_key_not_null" NOT NULL,
    "peer_spiffe_id" "text" NOT NULL,
    CONSTRAINT "authority_readback_attestati_semantic_request_digest_sha2_check" CHECK (("semantic_request_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_attestation_rec_evidence_digest_sha256_check" CHECK (("evidence_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_attestation_receipts_peer_spiffe_id_check" CHECK (("peer_spiffe_id" ~ '^spiffe://kodex[.]local/'::"text"))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_readback_attestation_receipts" FORCE ROW LEVEL SECURITY;



--
-- Name: authority_readback_intents; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_readback_intents" (
    "intent_id" "uuid" NOT NULL,
    "kind" "text" NOT NULL,
    "intent_revision" bigint NOT NULL,
    "intent_digest_sha256" "text" NOT NULL,
    "workload_id" "text" NOT NULL,
    "role" "text" NOT NULL,
    "workload_generation" bigint NOT NULL,
    "credential_generation" bigint NOT NULL,
    "possession_key_generation" bigint NOT NULL,
    "status" "text" NOT NULL,
    "expires_at" timestamp with time zone NOT NULL,
    "workload_spiffe_id" "text" NOT NULL,
    "material_generation" bigint NOT NULL,
    "possession_key_kid" "text" NOT NULL,
    "possession_key_generation_exact" bigint CONSTRAINT "authority_readback_intents_possession_key_generation_e_not_null" NOT NULL,
    "possession_public_jwk" "jsonb" NOT NULL,
    "possession_key_thumbprint_sha256" "text" CONSTRAINT "authority_readback_intents_possession_key_thumbprint_s_not_null" NOT NULL,
    "source_revision" bigint NOT NULL,
    "served_state_digest_sha256" "text" NOT NULL,
    CONSTRAINT "authority_readback_intents_intent_digest_sha256_check" CHECK (("intent_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_intents_kind_check" CHECK (("kind" = ANY (ARRAY['KEY_DELIVERY'::"text", 'SNAPSHOT'::"text"]))),
    CONSTRAINT "authority_readback_intents_material_generation_check" CHECK ((("material_generation" >= 1) AND ("material_generation" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_readback_intents_possession_key_generation_exac_check" CHECK ((("possession_key_generation_exact" >= 1) AND ("possession_key_generation_exact" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_readback_intents_possession_key_kid_check" CHECK (("possession_key_kid" ~ '^[A-Za-z0-9._-]{3,64}$'::"text")),
    CONSTRAINT "authority_readback_intents_possession_key_thumbprint_sha2_check" CHECK (("possession_key_thumbprint_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_intents_served_state_digest_sha256_check" CHECK (("served_state_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_intents_source_revision_check" CHECK ((("source_revision" >= 1) AND ("source_revision" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_readback_intents_status_check" CHECK (("status" = ANY (ARRAY['PINNED'::"text", 'ATTESTED'::"text", 'PROMOTED'::"text", 'EXPIRED'::"text"]))),
    CONSTRAINT "authority_readback_intents_workload_spiffe_id_check" CHECK (("workload_spiffe_id" ~ '^spiffe://kodex[.]local/'::"text"))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_readback_intents" FORCE ROW LEVEL SECURITY;



--
-- Name: authority_readback_trust_watermarks; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_readback_trust_watermarks" (
    "attestor_id" "text" NOT NULL,
    "root_id" "text" NOT NULL,
    "root_fingerprint_sha256" "text" CONSTRAINT "authority_readback_trust_water_root_fingerprint_sha256_not_null" NOT NULL,
    "manifest_bundle_revision" bigint CONSTRAINT "authority_readback_trust_wate_manifest_bundle_revision_not_null" NOT NULL,
    "manifest_bundle_digest_sha256" "text" CONSTRAINT "authority_readback_trust_wa_manifest_bundle_digest_sha_not_null" NOT NULL,
    "trust_source_revision" bigint CONSTRAINT "authority_readback_trust_waterma_trust_source_revision_not_null" NOT NULL,
    "trust_set_digest_sha256" "text" CONSTRAINT "authority_readback_trust_water_trust_set_digest_sha256_not_null" NOT NULL,
    "trust_key_set_revision" bigint CONSTRAINT "authority_readback_trust_waterm_trust_key_set_revision_not_null" NOT NULL,
    "signer_generation" bigint NOT NULL,
    "predecessor_state_digest_sha256" "text" CONSTRAINT "authority_readback_trust_wa_predecessor_state_digest_s_not_null" NOT NULL,
    "served_state_digest_sha256" "text" CONSTRAINT "authority_readback_trust_wa_served_state_digest_sha256_not_null" NOT NULL,
    "served_at" timestamp with time zone NOT NULL,
    CONSTRAINT "authority_readback_trust_wat_manifest_bundle_digest_sha25_check" CHECK (("manifest_bundle_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_trust_wat_predecessor_state_digest_sha_check" CHECK (("predecessor_state_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_trust_water_served_state_digest_sha256_check" CHECK (("served_state_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_trust_waterma_manifest_bundle_revision_check" CHECK ((("manifest_bundle_revision" >= 1) AND ("manifest_bundle_revision" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_readback_trust_watermar_root_fingerprint_sha256_check" CHECK (("root_fingerprint_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_trust_watermar_trust_set_digest_sha256_check" CHECK (("trust_set_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_readback_trust_watermark_trust_key_set_revision_check" CHECK ((("trust_key_set_revision" >= 1) AND ("trust_key_set_revision" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_readback_trust_watermarks_signer_generation_check" CHECK ((("signer_generation" >= 1) AND ("signer_generation" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_readback_trust_watermarks_trust_source_revision_check" CHECK ((("trust_source_revision" >= 1) AND ("trust_source_revision" <= '9007199254740991'::bigint)))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_readback_trust_watermarks" FORCE ROW LEVEL SECURITY;



--
-- Name: authority_replay_reservations; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_replay_reservations" (
    "target_workload_id" "text" NOT NULL,
    "jti" "uuid" NOT NULL,
    "canonical_digest_sha256" "text" NOT NULL,
    "expires_at" timestamp with time zone NOT NULL,
    "accepted_at" timestamp with time zone DEFAULT "clock_timestamp"() NOT NULL,
    CONSTRAINT "authority_replay_reservations_canonical_digest_sha256_check" CHECK (("canonical_digest_sha256" ~ '^[a-f0-9]{64}$'::"text"))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_replay_reservations" FORCE ROW LEVEL SECURITY;



--
-- Name: authority_restore_fences; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_restore_fences" (
    "database_cluster_id" "text" NOT NULL,
    "restore_epoch" bigint NOT NULL,
    "phase" "text" NOT NULL,
    "evidence_digest_sha256" "text" NOT NULL,
    "safe_window_not_before" timestamp with time zone,
    "updated_at" timestamp with time zone NOT NULL,
    CONSTRAINT "authority_restore_fences_evidence_digest_sha256_check" CHECK (("evidence_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_restore_fences_phase_check" CHECK (("phase" = ANY (ARRAY['OPEN'::"text", 'QUIESCING'::"text", 'PREPARED'::"text", 'RESTORING'::"text", 'COMPLETED'::"text", 'FENCED_SAFE_WINDOW'::"text"])))
);



--
-- Name: authority_rotation_intents; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_rotation_intents" (
    "intent_id" "uuid" NOT NULL,
    "source_revision" bigint NOT NULL,
    "source_digest_sha256" "text" NOT NULL,
    "status" "text" NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL,
    CONSTRAINT "authority_rotation_intents_source_digest_sha256_check" CHECK (("source_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_rotation_intents_status_check" CHECK (("status" = ANY (ARRAY['PREPARED'::"text", 'DELIVERED'::"text", 'PROMOTED'::"text", 'ABORTED'::"text"])))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_rotation_intents" FORCE ROW LEVEL SECURITY;



--
-- Name: authority_runtime_database_identities; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_runtime_database_identities" (
    "capability" "text" NOT NULL,
    "principal" "text" NOT NULL,
    "generation" bigint NOT NULL,
    "lifecycle_status" "text" NOT NULL,
    "registered_set_digest_sha256" "text" CONSTRAINT "authority_runtime_database__registered_set_digest_sha2_not_null" NOT NULL,
    "reconciled_at" timestamp with time zone NOT NULL,
    "retired_at" timestamp with time zone,
    CONSTRAINT "authority_runtime_database_i_registered_set_digest_sha256_check" CHECK (("registered_set_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_runtime_database_identities_capability_check" CHECK (("capability" = ANY (ARRAY['PUBLISHER'::"text", 'READBACK_ATTESTOR'::"text"]))),
    CONSTRAINT "authority_runtime_database_identities_generation_check" CHECK ((("generation" >= 1) AND ("generation" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_runtime_database_identities_lifecycle_status_check" CHECK (("lifecycle_status" = ANY (ARRAY['CURRENT'::"text", 'NEXT'::"text", 'PREVIOUS'::"text", 'RETIRED'::"text"])))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_runtime_database_identities" FORCE ROW LEVEL SECURITY;



--
-- Name: authority_snapshot_history; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_snapshot_history" (
    "source_revision" bigint NOT NULL,
    "source_digest_sha256" "text" NOT NULL,
    "key_set_revision" bigint NOT NULL,
    "policy_revision" bigint NOT NULL,
    "signer_generation" bigint NOT NULL,
    "predecessor_revision" bigint NOT NULL,
    "predecessor_digest_sha256" "text" NOT NULL,
    "canonical_payload" "jsonb" NOT NULL,
    "published_at" timestamp with time zone NOT NULL,
    "snapshot_compact_jws" "text",
    "publication_intent_id" "uuid",
    "publication_input_digest_sha256" "text",
    "expected_readback_count" integer,
    CONSTRAINT "authority_snapshot_history_expected_readback_count_check" CHECK ((("expected_readback_count" >= 1) AND ("expected_readback_count" <= 384))),
    CONSTRAINT "authority_snapshot_history_predecessor_digest_sha256_check" CHECK (("predecessor_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_snapshot_history_publication_input_digest_sha25_check" CHECK (("publication_input_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_snapshot_history_source_digest_sha256_check" CHECK (("source_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_snapshot_history_source_revision_check" CHECK ((("source_revision" >= 1) AND ("source_revision" <= '9007199254740991'::bigint)))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_snapshot_history" FORCE ROW LEVEL SECURITY;



--
-- Name: authority_snapshot_readbacks; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_snapshot_readbacks" (
    "readback_id" "uuid" NOT NULL,
    "workload_id" "text" NOT NULL,
    "role" "text" NOT NULL,
    "workload_generation" bigint NOT NULL,
    "source_revision" bigint NOT NULL,
    "digest_sha256" "text" NOT NULL,
    "verified_at" timestamp with time zone NOT NULL,
    CONSTRAINT "authority_snapshot_readbacks_digest_sha256_check" CHECK (("digest_sha256" ~ '^[a-f0-9]{64}$'::"text"))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_snapshot_readbacks" FORCE ROW LEVEL SECURITY;



--
-- Name: authority_snapshot_watermarks; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."authority_snapshot_watermarks" (
    "target_workload_id" "text" NOT NULL,
    "source_revision" bigint NOT NULL,
    "source_digest_sha256" "text" NOT NULL,
    "key_set_revision" bigint NOT NULL,
    "policy_revision" bigint NOT NULL,
    "signer_generation" bigint NOT NULL,
    "served_at" timestamp with time zone NOT NULL,
    "readback_attestation_receipt_id" "uuid",
    CONSTRAINT "authority_snapshot_watermarks_key_set_revision_check" CHECK ((("key_set_revision" >= 1) AND ("key_set_revision" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_snapshot_watermarks_policy_revision_check" CHECK ((("policy_revision" >= 1) AND ("policy_revision" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_snapshot_watermarks_signer_generation_check" CHECK ((("signer_generation" >= 1) AND ("signer_generation" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_snapshot_watermarks_source_digest_sha256_check" CHECK (("source_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "authority_snapshot_watermarks_source_revision_check" CHECK ((("source_revision" >= 1) AND ("source_revision" <= '9007199254740991'::bigint))),
    CONSTRAINT "authority_snapshot_watermarks_target_workload_id_check" CHECK (("target_workload_id" ~ '^[a-z0-9](?:[a-z0-9.-]{1,94}[a-z0-9])$'::"text"))
);

ALTER TABLE ONLY "internal_rpc_authority"."authority_snapshot_watermarks" FORCE ROW LEVEL SECURITY;



--
-- Name: database_credential_reconciler_leases; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."database_credential_reconciler_leases" (
    "lease_name" "text" NOT NULL,
    "holder_id" "uuid" NOT NULL,
    "fencing_token" bigint NOT NULL,
    "lease_until" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL,
    CONSTRAINT "database_credential_reconciler_leases_fencing_token_check" CHECK (("fencing_token" > 0)),
    CONSTRAINT "database_credential_reconciler_leases_lease_name_check" CHECK (("lease_name" = 'database-credential-reconciler'::"text"))
);

ALTER TABLE ONLY "internal_rpc_authority"."database_credential_reconciler_leases" FORCE ROW LEVEL SECURITY;



--
-- Name: database_credential_reconciliation_receipts; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."database_credential_reconciliation_receipts" (
    "request_id" "uuid" NOT NULL,
    "canonical_request_digest_sha256" "text" CONSTRAINT "database_credential_reconci_canonical_request_digest_s_not_null" NOT NULL,
    "reconciled_at" timestamp with time zone DEFAULT "clock_timestamp"() CONSTRAINT "database_credential_reconciliation_recei_reconciled_at_not_null" NOT NULL,
    CONSTRAINT "database_credential_reconcil_canonical_request_digest_sha_check" CHECK (("canonical_request_digest_sha256" ~ '^[a-f0-9]{64}$'::"text"))
);

ALTER TABLE ONLY "internal_rpc_authority"."database_credential_reconciliation_receipts" FORCE ROW LEVEL SECURITY;



--
-- Name: database_credential_rotation_intents; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."database_credential_rotation_intents" (
    "request_id" "uuid" NOT NULL,
    "canonical_digest_sha256" "text" CONSTRAINT "database_credential_rotation_i_canonical_digest_sha256_not_null" NOT NULL,
    "phase" "text" NOT NULL,
    "pre_rotation_digests" "jsonb" DEFAULT '{}'::"jsonb" CONSTRAINT "database_credential_rotation_inte_pre_rotation_digests_not_null" NOT NULL,
    "staged_digests" "jsonb" DEFAULT '{}'::"jsonb" NOT NULL,
    "created_at" timestamp with time zone DEFAULT "clock_timestamp"() NOT NULL,
    "updated_at" timestamp with time zone DEFAULT "clock_timestamp"() NOT NULL,
    CONSTRAINT "database_credential_rotation_inte_canonical_digest_sha256_check" CHECK (("canonical_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "database_credential_rotation_intents_phase_check" CHECK (("phase" = ANY (ARRAY['CREATED'::"text", 'PRE_ROTATE_CHECKPOINTED'::"text", 'NEXT_STAGED'::"text", 'NEXT_READ_BACK'::"text", 'PROMOTED'::"text", 'CURRENT_ROLLED_OUT'::"text", 'COMPLETED'::"text"]))),
    CONSTRAINT "database_credential_rotation_intents_pre_rotation_digests_check" CHECK (("jsonb_typeof"("pre_rotation_digests") = 'object'::"text")),
    CONSTRAINT "database_credential_rotation_intents_staged_digests_check" CHECK (("jsonb_typeof"("staged_digests") = 'object'::"text"))
);

ALTER TABLE ONLY "internal_rpc_authority"."database_credential_rotation_intents" FORCE ROW LEVEL SECURITY;



--
-- Name: database_credential_session_readbacks; Type: TABLE; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE TABLE "internal_rpc_authority"."database_credential_session_readbacks" (
    "capability" "text" NOT NULL,
    "generation" bigint NOT NULL,
    "lifecycle_status" "text" NOT NULL,
    "principal" "text" NOT NULL,
    "credential_digest_sha256" "text" CONSTRAINT "database_credential_session_r_credential_digest_sha256_not_null" NOT NULL,
    "pod_uid" "uuid" NOT NULL,
    "observed_at" timestamp with time zone DEFAULT "clock_timestamp"() NOT NULL,
    CONSTRAINT "database_credential_session_read_credential_digest_sha256_check" CHECK (("credential_digest_sha256" ~ '^[a-f0-9]{64}$'::"text")),
    CONSTRAINT "database_credential_session_readbacks_capability_check" CHECK (("capability" = ANY (ARRAY['PUBLISHER'::"text", 'READBACK_ATTESTOR'::"text"]))),
    CONSTRAINT "database_credential_session_readbacks_generation_check" CHECK ((("generation" >= 1) AND ("generation" <= '9007199254740991'::bigint))),
    CONSTRAINT "database_credential_session_readbacks_lifecycle_status_check" CHECK (("lifecycle_status" = ANY (ARRAY['CURRENT'::"text", 'NEXT'::"text"])))
);

ALTER TABLE ONLY "internal_rpc_authority"."database_credential_session_readbacks" FORCE ROW LEVEL SECURITY;



-- Name: authority_key_delivery_readbacks authority_key_delivery_readbacks_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_key_delivery_readbacks"
    ADD CONSTRAINT "authority_key_delivery_readbacks_pkey" PRIMARY KEY ("readback_id");


--
-- Name: authority_proof_reservations authority_proof_reservations_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_proof_reservations"
    ADD CONSTRAINT "authority_proof_reservations_pkey" PRIMARY KEY ("caller_workload_id", "jti");


--
-- Name: authority_proof_watermarks authority_proof_watermarks_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_proof_watermarks"
    ADD CONSTRAINT "authority_proof_watermarks_pkey" PRIMARY KEY ("caller_workload_id", "operation_id", "authority_proof_issuer");


--
-- Name: authority_publisher_delivery_receipts authority_publisher_delivery_receipts_directive_jti_key; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_publisher_delivery_receipts"
    ADD CONSTRAINT "authority_publisher_delivery_receipts_directive_jti_key" UNIQUE ("directive_jti");


--
-- Name: authority_publisher_delivery_receipts authority_publisher_delivery_receipts_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_publisher_delivery_receipts"
    ADD CONSTRAINT "authority_publisher_delivery_receipts_pkey" PRIMARY KEY ("idempotency_key");


--
-- Name: authority_readback_attestation_challenges authority_readback_attestation_challenges_challenge_jti_key; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_readback_attestation_challenges"
    ADD CONSTRAINT "authority_readback_attestation_challenges_challenge_jti_key" UNIQUE ("challenge_jti");


--
-- Name: authority_readback_attestation_challenges authority_readback_attestation_challenges_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_readback_attestation_challenges"
    ADD CONSTRAINT "authority_readback_attestation_challenges_pkey" PRIMARY KEY ("challenge_id");


--
-- Name: authority_readback_attestation_receipts authority_readback_attestation_receipts_challenge_id_key; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_readback_attestation_receipts"
    ADD CONSTRAINT "authority_readback_attestation_receipts_challenge_id_key" UNIQUE ("challenge_id");


--
-- Name: authority_readback_attestation_receipts authority_readback_attestation_receipts_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_readback_attestation_receipts"
    ADD CONSTRAINT "authority_readback_attestation_receipts_pkey" PRIMARY KEY ("receipt_id");


--
-- Name: authority_readback_intents authority_readback_intents_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_readback_intents"
    ADD CONSTRAINT "authority_readback_intents_pkey" PRIMARY KEY ("intent_id");


--
-- Name: authority_readback_trust_watermarks authority_readback_trust_watermarks_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_readback_trust_watermarks"
    ADD CONSTRAINT "authority_readback_trust_watermarks_pkey" PRIMARY KEY ("attestor_id");


--
-- Name: authority_replay_reservations authority_replay_reservations_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_replay_reservations"
    ADD CONSTRAINT "authority_replay_reservations_pkey" PRIMARY KEY ("target_workload_id", "jti");


--
-- Name: authority_restore_fences authority_restore_fences_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_restore_fences"
    ADD CONSTRAINT "authority_restore_fences_pkey" PRIMARY KEY ("database_cluster_id");


--
-- Name: authority_rotation_intents authority_rotation_intents_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_rotation_intents"
    ADD CONSTRAINT "authority_rotation_intents_pkey" PRIMARY KEY ("intent_id");


--
-- Name: authority_runtime_database_identities authority_runtime_database_identities_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_runtime_database_identities"
    ADD CONSTRAINT "authority_runtime_database_identities_pkey" PRIMARY KEY ("capability", "generation");


--
-- Name: authority_runtime_database_identities authority_runtime_database_identities_principal_key; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_runtime_database_identities"
    ADD CONSTRAINT "authority_runtime_database_identities_principal_key" UNIQUE ("principal");


--
-- Name: authority_snapshot_history authority_snapshot_history_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_snapshot_history"
    ADD CONSTRAINT "authority_snapshot_history_pkey" PRIMARY KEY ("source_revision");


--
-- Name: authority_snapshot_history authority_snapshot_history_publication_intent_id_key; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_snapshot_history"
    ADD CONSTRAINT "authority_snapshot_history_publication_intent_id_key" UNIQUE ("publication_intent_id");


--
-- Name: authority_snapshot_history authority_snapshot_history_source_digest_sha256_key; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_snapshot_history"
    ADD CONSTRAINT "authority_snapshot_history_source_digest_sha256_key" UNIQUE ("source_digest_sha256");


--
-- Name: authority_snapshot_readbacks authority_snapshot_readbacks_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_snapshot_readbacks"
    ADD CONSTRAINT "authority_snapshot_readbacks_pkey" PRIMARY KEY ("readback_id");


--
-- Name: authority_snapshot_watermarks authority_snapshot_watermarks_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_snapshot_watermarks"
    ADD CONSTRAINT "authority_snapshot_watermarks_pkey" PRIMARY KEY ("target_workload_id");


--
-- Name: database_credential_reconciler_leases database_credential_reconciler_leases_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."database_credential_reconciler_leases"
    ADD CONSTRAINT "database_credential_reconciler_leases_pkey" PRIMARY KEY ("lease_name");


--
-- Name: database_credential_reconciliation_receipts database_credential_reconciliation_receipts_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."database_credential_reconciliation_receipts"
    ADD CONSTRAINT "database_credential_reconciliation_receipts_pkey" PRIMARY KEY ("request_id");


--
-- Name: database_credential_rotation_intents database_credential_rotation_intents_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."database_credential_rotation_intents"
    ADD CONSTRAINT "database_credential_rotation_intents_pkey" PRIMARY KEY ("request_id");


--
-- Name: database_credential_session_readbacks database_credential_session_readbacks_pkey; Type: CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."database_credential_session_readbacks"
    ADD CONSTRAINT "database_credential_session_readbacks_pkey" PRIMARY KEY ("capability", "generation", "lifecycle_status", "pod_uid");


--
-- Name: authority_proof_reservations_expiry_idx; Type: INDEX; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE INDEX "authority_proof_reservations_expiry_idx" ON "internal_rpc_authority"."authority_proof_reservations" USING "btree" ("expires_at", "accepted_at");


--
-- Name: authority_readback_challenge_idempotency_idx; Type: INDEX; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE UNIQUE INDEX "authority_readback_challenge_idempotency_idx" ON "internal_rpc_authority"."authority_readback_attestation_challenges" USING "btree" ("peer_spiffe_id", "idempotency_key");


--
-- Name: authority_readback_receipt_evidence_jti_idx; Type: INDEX; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE UNIQUE INDEX "authority_readback_receipt_evidence_jti_idx" ON "internal_rpc_authority"."authority_readback_attestation_receipts" USING "btree" ("evidence_jti");


--
-- Name: authority_readback_receipt_idempotency_idx; Type: INDEX; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE UNIQUE INDEX "authority_readback_receipt_idempotency_idx" ON "internal_rpc_authority"."authority_readback_attestation_receipts" USING "btree" ("peer_spiffe_id", "idempotency_key");


--
-- Name: authority_replay_reservations_expiry_idx; Type: INDEX; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE INDEX "authority_replay_reservations_expiry_idx" ON "internal_rpc_authority"."authority_replay_reservations" USING "btree" ("expires_at", "accepted_at");


--
-- Name: authority_runtime_database_identities_current_idx; Type: INDEX; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE UNIQUE INDEX "authority_runtime_database_identities_current_idx" ON "internal_rpc_authority"."authority_runtime_database_identities" USING "btree" ("capability") WHERE ("lifecycle_status" = 'CURRENT'::"text");


--
-- Name: authority_runtime_database_identities_next_idx; Type: INDEX; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE UNIQUE INDEX "authority_runtime_database_identities_next_idx" ON "internal_rpc_authority"."authority_runtime_database_identities" USING "btree" ("capability") WHERE ("lifecycle_status" = 'NEXT'::"text");


--
-- Name: authority_snapshot_readbacks_exact_target_revision; Type: INDEX; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE UNIQUE INDEX "authority_snapshot_readbacks_exact_target_revision" ON "internal_rpc_authority"."authority_snapshot_readbacks" USING "btree" ("workload_id", "role", "workload_generation", "source_revision");


--
-- Name: authority_readback_attestation_challenges authority_readback_attestation_challenges_intent_id_fkey; Type: FK CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_readback_attestation_challenges"
    ADD CONSTRAINT "authority_readback_attestation_challenges_intent_id_fkey" FOREIGN KEY ("intent_id") REFERENCES "internal_rpc_authority"."authority_readback_intents"("intent_id");


--
-- Name: authority_readback_attestation_receipts authority_readback_attestation_receipts_challenge_id_fkey; Type: FK CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_readback_attestation_receipts"
    ADD CONSTRAINT "authority_readback_attestation_receipts_challenge_id_fkey" FOREIGN KEY ("challenge_id") REFERENCES "internal_rpc_authority"."authority_readback_attestation_challenges"("challenge_id");


--
-- Name: authority_snapshot_watermarks authority_snapshot_watermarks_readback_attestation_receipt_fkey; Type: FK CONSTRAINT; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE ONLY "internal_rpc_authority"."authority_snapshot_watermarks"
    ADD CONSTRAINT "authority_snapshot_watermarks_readback_attestation_receipt_fkey" FOREIGN KEY ("readback_attestation_receipt_id") REFERENCES "internal_rpc_authority"."authority_readback_attestation_receipts"("receipt_id");


--
-- Name: authority_proof_reservations; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_proof_reservations" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_proof_reservations authority_proof_reservations_issuer; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_proof_reservations_issuer" ON "internal_rpc_authority"."authority_proof_reservations" TO "internal_rpc_authority_issuer" USING (true) WITH CHECK (true);


--
-- Name: authority_proof_watermarks; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_proof_watermarks" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_proof_watermarks authority_proof_watermarks_issuer; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_proof_watermarks_issuer" ON "internal_rpc_authority"."authority_proof_watermarks" TO "internal_rpc_authority_issuer" USING (true) WITH CHECK (true);


--
-- Name: authority_publisher_delivery_receipts; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_publisher_delivery_receipts" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_publisher_delivery_receipts authority_publisher_delivery_receipts_runtime; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_publisher_delivery_receipts_runtime" ON "internal_rpc_authority"."authority_publisher_delivery_receipts" TO "internal_rpc_authority_publisher" USING (true) WITH CHECK (true);


--
-- Name: authority_readback_attestation_challenges; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_readback_attestation_challenges" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_readback_attestation_receipts; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_readback_attestation_receipts" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_readback_attestation_challenges authority_readback_challenges_attestor_read; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_readback_challenges_attestor_read" ON "internal_rpc_authority"."authority_readback_attestation_challenges" FOR SELECT TO "internal_rpc_authority_readback_attestor" USING ("internal_rpc_authority"."runtime_restore_fence_allows_work"());


--
-- Name: authority_readback_attestation_challenges authority_readback_challenges_owner_read; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_readback_challenges_owner_read" ON "internal_rpc_authority"."authority_readback_attestation_challenges" FOR SELECT TO "internal_rpc_authority_readback_owner" USING (true);


--
-- Name: authority_readback_attestation_challenges authority_readback_challenges_owner_write; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_readback_challenges_owner_write" ON "internal_rpc_authority"."authority_readback_attestation_challenges" TO "internal_rpc_authority_readback_owner" USING (true) WITH CHECK (true);


--
-- Name: authority_readback_intents; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_readback_intents" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_readback_intents authority_readback_intents_attestor; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_readback_intents_attestor" ON "internal_rpc_authority"."authority_readback_intents" FOR SELECT TO "internal_rpc_authority_readback_attestor" USING (true);


--
-- Name: authority_readback_intents authority_readback_intents_owner_lock; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_readback_intents_owner_lock" ON "internal_rpc_authority"."authority_readback_intents" FOR UPDATE TO "internal_rpc_authority_readback_owner" USING (true) WITH CHECK (true);


--
-- Name: authority_readback_intents authority_readback_intents_owner_read; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_readback_intents_owner_read" ON "internal_rpc_authority"."authority_readback_intents" FOR SELECT TO "internal_rpc_authority_readback_owner" USING (true);


--
-- Name: authority_readback_intents authority_readback_intents_publisher; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_readback_intents_publisher" ON "internal_rpc_authority"."authority_readback_intents" TO "internal_rpc_authority_publisher" USING (true) WITH CHECK ((("status" = 'PINNED'::"text") AND ("kind" = ANY (ARRAY['KEY_DELIVERY'::"text", 'SNAPSHOT'::"text"])) AND ("expires_at" > "clock_timestamp"()) AND "internal_rpc_authority"."runtime_restore_fence_allows_work"()));


--
-- Name: authority_readback_attestation_receipts authority_readback_receipts_attestor_read; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_readback_receipts_attestor_read" ON "internal_rpc_authority"."authority_readback_attestation_receipts" FOR SELECT TO "internal_rpc_authority_readback_attestor" USING (true);


--
-- Name: authority_readback_attestation_receipts authority_readback_receipts_owner_read; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_readback_receipts_owner_read" ON "internal_rpc_authority"."authority_readback_attestation_receipts" FOR SELECT TO "internal_rpc_authority_readback_owner" USING (true);


--
-- Name: authority_readback_attestation_receipts authority_readback_receipts_owner_write; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_readback_receipts_owner_write" ON "internal_rpc_authority"."authority_readback_attestation_receipts" TO "internal_rpc_authority_readback_owner" USING (true) WITH CHECK (true);


--
-- Name: authority_readback_trust_watermarks authority_readback_trust_attestor_read; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_readback_trust_attestor_read" ON "internal_rpc_authority"."authority_readback_trust_watermarks" FOR SELECT TO "internal_rpc_authority_readback_attestor" USING (true);


--
-- Name: authority_readback_trust_watermarks authority_readback_trust_owner; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_readback_trust_owner" ON "internal_rpc_authority"."authority_readback_trust_watermarks" TO "internal_rpc_authority_readback_owner" USING (true) WITH CHECK (true);


--
-- Name: authority_readback_trust_watermarks; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_readback_trust_watermarks" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_replay_reservations; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_replay_reservations" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_replay_reservations authority_replay_reservations_verifier; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_replay_reservations_verifier" ON "internal_rpc_authority"."authority_replay_reservations" TO "internal_rpc_authority_verifier" USING (true) WITH CHECK (true);


--
-- Name: authority_rotation_intents; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_rotation_intents" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_rotation_intents authority_rotation_intents_owner; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_rotation_intents_owner" ON "internal_rpc_authority"."authority_rotation_intents" TO "internal_rpc_authority_readback_owner" USING (true) WITH CHECK (true);


--
-- Name: authority_rotation_intents authority_rotation_intents_publisher_read; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_rotation_intents_publisher_read" ON "internal_rpc_authority"."authority_rotation_intents" FOR SELECT TO "internal_rpc_authority_publisher" USING (true);


--
-- Name: authority_runtime_database_identities; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_runtime_database_identities" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_runtime_database_identities authority_runtime_database_identities_lifecycle_definer; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_runtime_database_identities_lifecycle_definer" ON "internal_rpc_authority"."authority_runtime_database_identities" TO "internal_rpc_authority_credential_lifecycle_definer" USING (true) WITH CHECK (true);


--
-- Name: authority_runtime_database_identities authority_runtime_database_identities_owner; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_runtime_database_identities_owner" ON "internal_rpc_authority"."authority_runtime_database_identities" TO "internal_rpc_authority_readback_owner" USING (true) WITH CHECK (true);


--
-- Name: authority_runtime_database_identities authority_runtime_database_identities_publisher_read; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_runtime_database_identities_publisher_read" ON "internal_rpc_authority"."authority_runtime_database_identities" FOR SELECT TO "internal_rpc_authority_publisher" USING ((("capability" = 'PUBLISHER'::"text") AND ("principal" = SESSION_USER) AND ("lifecycle_status" = ANY (ARRAY['CURRENT'::"text", 'NEXT'::"text", 'PREVIOUS'::"text"]))));


--
-- Name: authority_runtime_database_identities authority_runtime_database_identities_reconciler_read; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_runtime_database_identities_reconciler_read" ON "internal_rpc_authority"."authority_runtime_database_identities" FOR SELECT TO "internal_rpc_authority_database_credential_reconciler" USING (true);


--
-- Name: authority_snapshot_history; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_snapshot_history" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_snapshot_history authority_snapshot_history_owner; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_snapshot_history_owner" ON "internal_rpc_authority"."authority_snapshot_history" TO "internal_rpc_authority_readback_owner" USING (true) WITH CHECK (true);


--
-- Name: authority_snapshot_history authority_snapshot_history_publisher_read; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_snapshot_history_publisher_read" ON "internal_rpc_authority"."authority_snapshot_history" FOR SELECT TO "internal_rpc_authority_publisher" USING (true);


--
-- Name: authority_snapshot_readbacks; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_snapshot_readbacks" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_snapshot_readbacks authority_snapshot_readbacks_owner; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_snapshot_readbacks_owner" ON "internal_rpc_authority"."authority_snapshot_readbacks" TO "internal_rpc_authority_readback_owner" USING (true) WITH CHECK (true);


--
-- Name: authority_snapshot_readbacks authority_snapshot_readbacks_publisher_read; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_snapshot_readbacks_publisher_read" ON "internal_rpc_authority"."authority_snapshot_readbacks" FOR SELECT TO "internal_rpc_authority_publisher" USING (true);


--
-- Name: authority_snapshot_watermarks; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."authority_snapshot_watermarks" ENABLE ROW LEVEL SECURITY;

--
-- Name: authority_snapshot_watermarks authority_snapshot_watermarks_runtime; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "authority_snapshot_watermarks_runtime" ON "internal_rpc_authority"."authority_snapshot_watermarks" TO "internal_rpc_authority_issuer", "internal_rpc_authority_verifier" USING (true) WITH CHECK (true);


--
-- Name: database_credential_reconciliation_receipts database_credential_receipts_lifecycle_definer; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "database_credential_receipts_lifecycle_definer" ON "internal_rpc_authority"."database_credential_reconciliation_receipts" TO "internal_rpc_authority_credential_lifecycle_definer" USING (true) WITH CHECK (true);


--
-- Name: database_credential_reconciler_leases; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."database_credential_reconciler_leases" ENABLE ROW LEVEL SECURITY;

--
-- Name: database_credential_reconciler_leases database_credential_reconciler_leases_runtime; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "database_credential_reconciler_leases_runtime" ON "internal_rpc_authority"."database_credential_reconciler_leases" TO "internal_rpc_authority_database_credential_reconciler" USING (true) WITH CHECK (true);


--
-- Name: database_credential_reconciliation_receipts; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."database_credential_reconciliation_receipts" ENABLE ROW LEVEL SECURITY;

--
-- Name: database_credential_reconciliation_receipts database_credential_reconciliation_receipts_owner; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "database_credential_reconciliation_receipts_owner" ON "internal_rpc_authority"."database_credential_reconciliation_receipts" TO "internal_rpc_authority_readback_owner" USING (true) WITH CHECK (true);


--
-- Name: database_credential_rotation_intents; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."database_credential_rotation_intents" ENABLE ROW LEVEL SECURITY;

--
-- Name: database_credential_rotation_intents database_credential_rotation_intents_owner; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "database_credential_rotation_intents_owner" ON "internal_rpc_authority"."database_credential_rotation_intents" TO "internal_rpc_authority_readback_owner" USING (true) WITH CHECK (true);


--
-- Name: database_credential_rotation_intents database_credential_rotation_intents_reconciler; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "database_credential_rotation_intents_reconciler" ON "internal_rpc_authority"."database_credential_rotation_intents" TO "internal_rpc_authority_database_credential_reconciler" USING (true) WITH CHECK (true);


--
-- Name: database_credential_session_readbacks; Type: ROW SECURITY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

ALTER TABLE "internal_rpc_authority"."database_credential_session_readbacks" ENABLE ROW LEVEL SECURITY;

--
-- Name: database_credential_session_readbacks database_credential_session_readbacks_owner; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "database_credential_session_readbacks_owner" ON "internal_rpc_authority"."database_credential_session_readbacks" TO "internal_rpc_authority_readback_owner" USING (true) WITH CHECK (true);


--
-- Name: database_credential_session_readbacks database_credential_session_readbacks_reconciler_read; Type: POLICY; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

CREATE POLICY "database_credential_session_readbacks_reconciler_read" ON "internal_rpc_authority"."database_credential_session_readbacks" FOR SELECT TO "internal_rpc_authority_database_credential_reconciler" USING (true);


--
-- Name: FUNCTION "activate_readback_trust"("p_root_id" "text", "p_root_fingerprint_sha256" "text", "p_manifest_bundle_revision" bigint, "p_manifest_bundle_digest_sha256" "text", "p_trust_source_revision" bigint, "p_trust_set_digest_sha256" "text", "p_trust_key_set_revision" bigint, "p_signer_generation" bigint, "p_predecessor_state_digest_sha256" "text", "p_served_state_digest_sha256" "text", "p_served_at" timestamp with time zone); Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

REVOKE ALL ON FUNCTION "internal_rpc_authority"."activate_readback_trust"("p_root_id" "text", "p_root_fingerprint_sha256" "text", "p_manifest_bundle_revision" bigint, "p_manifest_bundle_digest_sha256" "text", "p_trust_source_revision" bigint, "p_trust_set_digest_sha256" "text", "p_trust_key_set_revision" bigint, "p_signer_generation" bigint, "p_predecessor_state_digest_sha256" "text", "p_served_state_digest_sha256" "text", "p_served_at" timestamp with time zone) FROM PUBLIC;
GRANT ALL ON FUNCTION "internal_rpc_authority"."activate_readback_trust"("p_root_id" "text", "p_root_fingerprint_sha256" "text", "p_manifest_bundle_revision" bigint, "p_manifest_bundle_digest_sha256" "text", "p_trust_source_revision" bigint, "p_trust_set_digest_sha256" "text", "p_trust_key_set_revision" bigint, "p_signer_generation" bigint, "p_predecessor_state_digest_sha256" "text", "p_served_state_digest_sha256" "text", "p_served_at" timestamp with time zone) TO "internal_rpc_authority_readback_attestor";


--
-- Name: FUNCTION "apply_restore_fence"("p_database_cluster_id" "text", "p_restore_epoch" bigint, "p_phase" "text", "p_evidence_digest_sha256" "text", "p_safe_window_not_before" timestamp with time zone); Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

REVOKE ALL ON FUNCTION "internal_rpc_authority"."apply_restore_fence"("p_database_cluster_id" "text", "p_restore_epoch" bigint, "p_phase" "text", "p_evidence_digest_sha256" "text", "p_safe_window_not_before" timestamp with time zone) FROM PUBLIC;
GRANT ALL ON FUNCTION "internal_rpc_authority"."apply_restore_fence"("p_database_cluster_id" "text", "p_restore_epoch" bigint, "p_phase" "text", "p_evidence_digest_sha256" "text", "p_safe_window_not_before" timestamp with time zone) TO "internal_rpc_authority_restore_controller";


--
-- Name: FUNCTION "consume_authority_readback_attestation_challenge"("p_challenge_id" "uuid", "p_receipt_id" "uuid", "p_evidence_jti" "uuid", "p_evidence_digest_sha256" "text", "p_verifier_generation" bigint, "p_idempotency_key" "uuid", "p_semantic_request_digest_sha256" "text"); Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

REVOKE ALL ON FUNCTION "internal_rpc_authority"."consume_authority_readback_attestation_challenge"("p_challenge_id" "uuid", "p_receipt_id" "uuid", "p_evidence_jti" "uuid", "p_evidence_digest_sha256" "text", "p_verifier_generation" bigint, "p_idempotency_key" "uuid", "p_semantic_request_digest_sha256" "text") FROM PUBLIC;
GRANT ALL ON FUNCTION "internal_rpc_authority"."consume_authority_readback_attestation_challenge"("p_challenge_id" "uuid", "p_receipt_id" "uuid", "p_evidence_jti" "uuid", "p_evidence_digest_sha256" "text", "p_verifier_generation" bigint, "p_idempotency_key" "uuid", "p_semantic_request_digest_sha256" "text") TO "internal_rpc_authority_readback_attestor";


--
-- Name: FUNCTION "issue_authority_readback_attestation_challenge"("p_intent_id" "uuid", "p_challenge_id" "uuid", "p_challenge_jti" "uuid", "p_challenge_nonce" "text", "p_challenge_digest_sha256" "text", "p_readback_credential_jti" "uuid", "p_readback_credential_digest_sha256" "text", "p_idempotency_key" "uuid", "p_semantic_request_digest_sha256" "text"); Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

REVOKE ALL ON FUNCTION "internal_rpc_authority"."issue_authority_readback_attestation_challenge"("p_intent_id" "uuid", "p_challenge_id" "uuid", "p_challenge_jti" "uuid", "p_challenge_nonce" "text", "p_challenge_digest_sha256" "text", "p_readback_credential_jti" "uuid", "p_readback_credential_digest_sha256" "text", "p_idempotency_key" "uuid", "p_semantic_request_digest_sha256" "text") FROM PUBLIC;
GRANT ALL ON FUNCTION "internal_rpc_authority"."issue_authority_readback_attestation_challenge"("p_intent_id" "uuid", "p_challenge_id" "uuid", "p_challenge_jti" "uuid", "p_challenge_nonce" "text", "p_challenge_digest_sha256" "text", "p_readback_credential_jti" "uuid", "p_readback_credential_digest_sha256" "text", "p_idempotency_key" "uuid", "p_semantic_request_digest_sha256" "text") TO "internal_rpc_authority_readback_attestor";


--
-- Name: FUNCTION "publisher_append_snapshot_history"("p_source_revision" bigint, "p_source_digest_sha256" "text", "p_key_set_revision" bigint, "p_policy_revision" bigint, "p_signer_generation" bigint, "p_predecessor_revision" bigint, "p_predecessor_digest_sha256" "text", "p_snapshot_compact_jws" "text", "p_publication_intent_id" "uuid", "p_publication_input_digest_sha256" "text", "p_expected_readback_count" integer, "p_published_at" timestamp with time zone); Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

REVOKE ALL ON FUNCTION "internal_rpc_authority"."publisher_append_snapshot_history"("p_source_revision" bigint, "p_source_digest_sha256" "text", "p_key_set_revision" bigint, "p_policy_revision" bigint, "p_signer_generation" bigint, "p_predecessor_revision" bigint, "p_predecessor_digest_sha256" "text", "p_snapshot_compact_jws" "text", "p_publication_intent_id" "uuid", "p_publication_input_digest_sha256" "text", "p_expected_readback_count" integer, "p_published_at" timestamp with time zone) FROM PUBLIC;
GRANT ALL ON FUNCTION "internal_rpc_authority"."publisher_append_snapshot_history"("p_source_revision" bigint, "p_source_digest_sha256" "text", "p_key_set_revision" bigint, "p_policy_revision" bigint, "p_signer_generation" bigint, "p_predecessor_revision" bigint, "p_predecessor_digest_sha256" "text", "p_snapshot_compact_jws" "text", "p_publication_intent_id" "uuid", "p_publication_input_digest_sha256" "text", "p_expected_readback_count" integer, "p_published_at" timestamp with time zone) TO "internal_rpc_authority_publisher";


--
-- Name: FUNCTION "publisher_promote_snapshot"("p_publication_intent_id" "uuid", "p_source_revision" bigint, "p_source_digest_sha256" "text", "p_expected_readback_count" integer); Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

REVOKE ALL ON FUNCTION "internal_rpc_authority"."publisher_promote_snapshot"("p_publication_intent_id" "uuid", "p_source_revision" bigint, "p_source_digest_sha256" "text", "p_expected_readback_count" integer) FROM PUBLIC;
GRANT ALL ON FUNCTION "internal_rpc_authority"."publisher_promote_snapshot"("p_publication_intent_id" "uuid", "p_source_revision" bigint, "p_source_digest_sha256" "text", "p_expected_readback_count" integer) TO "internal_rpc_authority_publisher";


--
-- Name: FUNCTION "reconcile_runtime_database_identity"("requested_capability" "text", "requested_principal" "text", "requested_generation" bigint, "requested_status" "text", "requested_request_id" "uuid", "requested_registered_set_digest_sha256" "text"); Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_credential_lifecycle_definer
--

REVOKE ALL ON FUNCTION "internal_rpc_authority"."reconcile_runtime_database_identity"("requested_capability" "text", "requested_principal" "text", "requested_generation" bigint, "requested_status" "text", "requested_request_id" "uuid", "requested_registered_set_digest_sha256" "text") FROM PUBLIC;
GRANT ALL ON FUNCTION "internal_rpc_authority"."reconcile_runtime_database_identity"("requested_capability" "text", "requested_principal" "text", "requested_generation" bigint, "requested_status" "text", "requested_request_id" "uuid", "requested_registered_set_digest_sha256" "text") TO "internal_rpc_authority_database_credential_reconciler";


--
-- Name: FUNCTION "record_database_credential_session_readback"("p_credential_digest_sha256" "text", "p_pod_uid" "uuid"); Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

REVOKE ALL ON FUNCTION "internal_rpc_authority"."record_database_credential_session_readback"("p_credential_digest_sha256" "text", "p_pod_uid" "uuid") FROM PUBLIC;
GRANT ALL ON FUNCTION "internal_rpc_authority"."record_database_credential_session_readback"("p_credential_digest_sha256" "text", "p_pod_uid" "uuid") TO "internal_rpc_authority_publisher";
GRANT ALL ON FUNCTION "internal_rpc_authority"."record_database_credential_session_readback"("p_credential_digest_sha256" "text", "p_pod_uid" "uuid") TO "internal_rpc_authority_readback_attestor";


--
-- Name: FUNCTION "retire_runtime_database_identity"("requested_capability" "text", "requested_principal" "text", "requested_generation" bigint, "requested_request_id" "uuid", "requested_registered_set_digest_sha256" "text"); Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_credential_lifecycle_definer
--

REVOKE ALL ON FUNCTION "internal_rpc_authority"."retire_runtime_database_identity"("requested_capability" "text", "requested_principal" "text", "requested_generation" bigint, "requested_request_id" "uuid", "requested_registered_set_digest_sha256" "text") FROM PUBLIC;
GRANT ALL ON FUNCTION "internal_rpc_authority"."retire_runtime_database_identity"("requested_capability" "text", "requested_principal" "text", "requested_generation" bigint, "requested_request_id" "uuid", "requested_registered_set_digest_sha256" "text") TO "internal_rpc_authority_database_credential_reconciler";


--
-- Name: FUNCTION "runtime_restore_fence_allows_work"(); Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

REVOKE ALL ON FUNCTION "internal_rpc_authority"."runtime_restore_fence_allows_work"() FROM PUBLIC;
GRANT ALL ON FUNCTION "internal_rpc_authority"."runtime_restore_fence_allows_work"() TO "internal_rpc_authority_issuer";
GRANT ALL ON FUNCTION "internal_rpc_authority"."runtime_restore_fence_allows_work"() TO "internal_rpc_authority_verifier";
GRANT ALL ON FUNCTION "internal_rpc_authority"."runtime_restore_fence_allows_work"() TO "internal_rpc_authority_publisher";
GRANT ALL ON FUNCTION "internal_rpc_authority"."runtime_restore_fence_allows_work"() TO "internal_rpc_authority_readback_attestor";


--
-- Name: FUNCTION "validate_snapshot_attestation_receipt"("p_receipt_id" "uuid", "p_workload_id" "text", "p_source_revision" bigint, "p_source_digest_sha256" "text"); Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

REVOKE ALL ON FUNCTION "internal_rpc_authority"."validate_snapshot_attestation_receipt"("p_receipt_id" "uuid", "p_workload_id" "text", "p_source_revision" bigint, "p_source_digest_sha256" "text") FROM PUBLIC;
GRANT ALL ON FUNCTION "internal_rpc_authority"."validate_snapshot_attestation_receipt"("p_receipt_id" "uuid", "p_workload_id" "text", "p_source_revision" bigint, "p_source_digest_sha256" "text") TO "internal_rpc_authority_issuer";
GRANT ALL ON FUNCTION "internal_rpc_authority"."validate_snapshot_attestation_receipt"("p_receipt_id" "uuid", "p_workload_id" "text", "p_source_revision" bigint, "p_source_digest_sha256" "text") TO "internal_rpc_authority_verifier";


--
-- Name: TABLE "authority_proof_reservations"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT,INSERT,DELETE ON TABLE "internal_rpc_authority"."authority_proof_reservations" TO "internal_rpc_authority_issuer";


--
-- Name: TABLE "authority_proof_watermarks"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT,INSERT,UPDATE ON TABLE "internal_rpc_authority"."authority_proof_watermarks" TO "internal_rpc_authority_issuer";


--
-- Name: TABLE "authority_publisher_delivery_receipts"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT,INSERT ON TABLE "internal_rpc_authority"."authority_publisher_delivery_receipts" TO "internal_rpc_authority_publisher";


--
-- Name: TABLE "authority_readback_attestation_challenges"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT ON TABLE "internal_rpc_authority"."authority_readback_attestation_challenges" TO "internal_rpc_authority_readback_attestor";


--
-- Name: TABLE "authority_readback_attestation_receipts"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT ON TABLE "internal_rpc_authority"."authority_readback_attestation_receipts" TO "internal_rpc_authority_readback_attestor";


--
-- Name: TABLE "authority_readback_intents"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT ON TABLE "internal_rpc_authority"."authority_readback_intents" TO "internal_rpc_authority_readback_attestor";
GRANT SELECT,INSERT ON TABLE "internal_rpc_authority"."authority_readback_intents" TO "internal_rpc_authority_publisher";


--
-- Name: TABLE "authority_readback_trust_watermarks"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT ON TABLE "internal_rpc_authority"."authority_readback_trust_watermarks" TO "internal_rpc_authority_readback_attestor";


--
-- Name: TABLE "authority_replay_reservations"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT,INSERT,DELETE ON TABLE "internal_rpc_authority"."authority_replay_reservations" TO "internal_rpc_authority_verifier";


--
-- Name: TABLE "authority_restore_fences"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT ON TABLE "internal_rpc_authority"."authority_restore_fences" TO "internal_rpc_authority_restore_controller";


--
-- Name: TABLE "authority_rotation_intents"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT ON TABLE "internal_rpc_authority"."authority_rotation_intents" TO "internal_rpc_authority_publisher";


--
-- Name: TABLE "authority_runtime_database_identities"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT ON TABLE "internal_rpc_authority"."authority_runtime_database_identities" TO "internal_rpc_authority_database_credential_reconciler";
GRANT SELECT,INSERT,UPDATE ON TABLE "internal_rpc_authority"."authority_runtime_database_identities" TO "internal_rpc_authority_credential_lifecycle_definer";
GRANT SELECT ON TABLE "internal_rpc_authority"."authority_runtime_database_identities" TO "internal_rpc_authority_publisher";


--
-- Name: TABLE "authority_snapshot_history"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT ON TABLE "internal_rpc_authority"."authority_snapshot_history" TO "internal_rpc_authority_publisher";


--
-- Name: TABLE "authority_snapshot_readbacks"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT ON TABLE "internal_rpc_authority"."authority_snapshot_readbacks" TO "internal_rpc_authority_publisher";


--
-- Name: TABLE "authority_snapshot_watermarks"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT,INSERT,UPDATE ON TABLE "internal_rpc_authority"."authority_snapshot_watermarks" TO "internal_rpc_authority_issuer";
GRANT SELECT,INSERT,UPDATE ON TABLE "internal_rpc_authority"."authority_snapshot_watermarks" TO "internal_rpc_authority_verifier";


--
-- Name: TABLE "database_credential_reconciler_leases"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT,INSERT,UPDATE ON TABLE "internal_rpc_authority"."database_credential_reconciler_leases" TO "internal_rpc_authority_database_credential_reconciler";


--
-- Name: TABLE "database_credential_reconciliation_receipts"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT,INSERT,UPDATE ON TABLE "internal_rpc_authority"."database_credential_reconciliation_receipts" TO "internal_rpc_authority_credential_lifecycle_definer";


--
-- Name: TABLE "database_credential_rotation_intents"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT,INSERT,UPDATE ON TABLE "internal_rpc_authority"."database_credential_rotation_intents" TO "internal_rpc_authority_database_credential_reconciler";


--
-- Name: TABLE "database_credential_session_readbacks"; Type: ACL; Schema: internal_rpc_authority; Owner: internal_rpc_authority_readback_owner
--

GRANT SELECT ON TABLE "internal_rpc_authority"."database_credential_session_readbacks" TO "internal_rpc_authority_database_credential_reconciler";

ALTER FUNCTION "internal_rpc_authority"."activate_readback_trust"("p_root_id" "text", "p_root_fingerprint_sha256" "text", "p_manifest_bundle_revision" bigint, "p_manifest_bundle_digest_sha256" "text", "p_trust_source_revision" bigint, "p_trust_set_digest_sha256" "text", "p_trust_key_set_revision" bigint, "p_signer_generation" bigint, "p_predecessor_state_digest_sha256" "text", "p_served_state_digest_sha256" "text", "p_served_at" timestamp with time zone) OWNER TO "internal_rpc_authority_readback_owner";
ALTER FUNCTION "internal_rpc_authority"."apply_restore_fence"("p_database_cluster_id" "text", "p_restore_epoch" bigint, "p_phase" "text", "p_evidence_digest_sha256" "text", "p_safe_window_not_before" timestamp with time zone) OWNER TO "internal_rpc_authority_readback_owner";
ALTER FUNCTION "internal_rpc_authority"."consume_authority_readback_attestation_challenge"("p_challenge_id" "uuid", "p_receipt_id" "uuid", "p_evidence_jti" "uuid", "p_evidence_digest_sha256" "text", "p_verifier_generation" bigint, "p_idempotency_key" "uuid", "p_semantic_request_digest_sha256" "text") OWNER TO "internal_rpc_authority_readback_owner";
ALTER FUNCTION "internal_rpc_authority"."issue_authority_readback_attestation_challenge"("p_intent_id" "uuid", "p_challenge_id" "uuid", "p_challenge_jti" "uuid", "p_challenge_nonce" "text", "p_challenge_digest_sha256" "text", "p_readback_credential_jti" "uuid", "p_readback_credential_digest_sha256" "text", "p_idempotency_key" "uuid", "p_semantic_request_digest_sha256" "text") OWNER TO "internal_rpc_authority_readback_owner";
ALTER FUNCTION "internal_rpc_authority"."publisher_append_snapshot_history"("p_source_revision" bigint, "p_source_digest_sha256" "text", "p_key_set_revision" bigint, "p_policy_revision" bigint, "p_signer_generation" bigint, "p_predecessor_revision" bigint, "p_predecessor_digest_sha256" "text", "p_snapshot_compact_jws" "text", "p_publication_intent_id" "uuid", "p_publication_input_digest_sha256" "text", "p_expected_readback_count" integer, "p_published_at" timestamp with time zone) OWNER TO "internal_rpc_authority_readback_owner";
ALTER FUNCTION "internal_rpc_authority"."publisher_promote_snapshot"("p_publication_intent_id" "uuid", "p_source_revision" bigint, "p_source_digest_sha256" "text", "p_expected_readback_count" integer) OWNER TO "internal_rpc_authority_readback_owner";
ALTER FUNCTION "internal_rpc_authority"."reconcile_runtime_database_identity"("requested_capability" "text", "requested_principal" "text", "requested_generation" bigint, "requested_status" "text", "requested_request_id" "uuid", "requested_registered_set_digest_sha256" "text") OWNER TO "internal_rpc_authority_credential_lifecycle_definer";
ALTER FUNCTION "internal_rpc_authority"."record_database_credential_session_readback"("p_credential_digest_sha256" "text", "p_pod_uid" "uuid") OWNER TO "internal_rpc_authority_readback_owner";
ALTER FUNCTION "internal_rpc_authority"."retire_runtime_database_identity"("requested_capability" "text", "requested_principal" "text", "requested_generation" bigint, "requested_request_id" "uuid", "requested_registered_set_digest_sha256" "text") OWNER TO "internal_rpc_authority_credential_lifecycle_definer";
ALTER FUNCTION "internal_rpc_authority"."runtime_restore_fence_allows_work"() OWNER TO "internal_rpc_authority_readback_owner";
ALTER FUNCTION "internal_rpc_authority"."validate_snapshot_attestation_receipt"("p_receipt_id" "uuid", "p_workload_id" "text", "p_source_revision" bigint, "p_source_digest_sha256" "text") OWNER TO "internal_rpc_authority_readback_owner";

ALTER TABLE "internal_rpc_authority"."authority_key_delivery_readbacks" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_proof_reservations" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_proof_watermarks" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_publisher_delivery_receipts" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_readback_attestation_challenges" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_readback_attestation_receipts" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_readback_intents" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_readback_trust_watermarks" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_replay_reservations" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_restore_fences" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_rotation_intents" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_runtime_database_identities" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_snapshot_history" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_snapshot_readbacks" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."authority_snapshot_watermarks" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."database_credential_reconciler_leases" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."database_credential_reconciliation_receipts" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."database_credential_rotation_intents" OWNER TO "internal_rpc_authority_readback_owner";
ALTER TABLE "internal_rpc_authority"."database_credential_session_readbacks" OWNER TO "internal_rpc_authority_readback_owner";

SET ROLE internal_rpc_authority_readback_owner;

INSERT INTO internal_rpc_authority.authority_restore_fences (
    database_cluster_id,
    restore_epoch,
    phase,
    evidence_digest_sha256,
    safe_window_not_before,
    updated_at
)
VALUES (
    'internal-rpc-authority-primary',
    1,
    'OPEN',
    '0000000000000000000000000000000000000000000000000000000000000000',
    NULL,
    pg_catalog.clock_timestamp()
);

REVOKE CREATE ON SCHEMA internal_rpc_authority
    FROM internal_rpc_authority_migrator,
         internal_rpc_authority_credential_lifecycle_definer;

RESET ROLE;
