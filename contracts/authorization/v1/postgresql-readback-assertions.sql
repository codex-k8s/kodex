\set ON_ERROR_STOP on

DO $assertion$
DECLARE
  owner_row pg_catalog.pg_roles%ROWTYPE;
  role_name text;
BEGIN
  FOREACH role_name IN ARRAY ARRAY[
    'internal_rpc_authority_readback_owner',
    'internal_rpc_authority_publisher',
    'internal_rpc_authority_readback_attestor'
  ]
  LOOP
    SELECT * INTO STRICT owner_row
      FROM pg_catalog.pg_roles
     WHERE rolname = role_name;
    IF owner_row.rolcanlogin OR owner_row.rolsuper OR owner_row.rolbypassrls
       OR owner_row.rolcreatedb OR owner_row.rolcreaterole
       OR owner_row.rolinherit THEN
      RAISE EXCEPTION 'protected role privileges are not minimal';
    END IF;
  END LOOP;
  FOREACH role_name IN ARRAY ARRAY[
    'ira_publisher_g1',
    'ira_publisher_g2'
  ]
  LOOP
    SELECT * INTO STRICT owner_row
      FROM pg_catalog.pg_roles
     WHERE rolname = role_name;
    IF NOT owner_row.rolcanlogin OR owner_row.rolsuper
       OR owner_row.rolbypassrls OR owner_row.rolcreatedb
       OR owner_row.rolcreaterole OR owner_row.rolinherit THEN
      RAISE EXCEPTION 'publisher login principal privileges are not minimal';
    END IF;
    IF NOT pg_catalog.pg_has_role(
      role_name,
      'internal_rpc_authority_publisher',
      'MEMBER'
    ) OR pg_catalog.pg_has_role(
      role_name,
      'internal_rpc_authority_readback_owner',
      'MEMBER'
    ) THEN
      RAISE EXCEPTION 'publisher login membership is not exact';
    END IF;
  END LOOP;
  FOREACH role_name IN ARRAY ARRAY[
    'ira_readback_attestor_g1',
    'ira_readback_attestor_g2'
  ]
  LOOP
    SELECT * INTO STRICT owner_row
      FROM pg_catalog.pg_roles
     WHERE rolname = role_name;
    IF NOT owner_row.rolcanlogin OR owner_row.rolsuper
       OR owner_row.rolbypassrls OR owner_row.rolcreatedb
       OR owner_row.rolcreaterole OR owner_row.rolinherit THEN
      RAISE EXCEPTION 'attestor login principal privileges are not minimal';
    END IF;
    IF NOT pg_catalog.pg_has_role(
      role_name,
      'internal_rpc_authority_readback_attestor',
      'MEMBER'
    ) OR pg_catalog.pg_has_role(
      role_name,
      'internal_rpc_authority_readback_owner',
      'MEMBER'
    ) THEN
      RAISE EXCEPTION 'attestor login membership is not exact';
    END IF;
  END LOOP;
  IF pg_catalog.has_schema_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority',
    'CREATE'
  ) OR pg_catalog.has_schema_privilege(
    'internal_rpc_authority_readback_attestor',
    'internal_rpc_authority',
    'CREATE'
  ) THEN
    RAISE EXCEPTION 'runtime role can create protected schema objects';
  END IF;
  IF pg_catalog.pg_has_role(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority_readback_owner',
    'MEMBER'
  ) OR pg_catalog.pg_has_role(
    'internal_rpc_authority_readback_attestor',
    'internal_rpc_authority_readback_owner',
    'MEMBER'
  ) OR pg_catalog.pg_has_role(
    'ira_control_plane_verifier_g1',
    'internal_rpc_authority_readback_owner',
    'MEMBER'
  ) THEN
    RAISE EXCEPTION 'runtime role is a member of readback owner';
  END IF;
END
$assertion$;

DO $assertion$
DECLARE
  invalid_table_count integer;
  function_count integer;
  lifecycle_index_count integer;
  trigger_count integer;
BEGIN
  SELECT count(*) INTO invalid_table_count
    FROM pg_catalog.pg_class AS relation
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = relation.relnamespace
   WHERE namespace.nspname = 'internal_rpc_authority'
     AND relation.relname IN (
       'authority_readback_intents',
       'authority_readback_attestation_challenges',
       'authority_readback_attestation_receipts',
       'authority_key_delivery_readbacks',
       'authority_snapshot_readbacks'
     )
     AND (NOT relation.relrowsecurity OR NOT relation.relforcerowsecurity);
  IF invalid_table_count <> 0 THEN
    RAISE EXCEPTION 'protected readback tables do not force RLS';
  END IF;

  SELECT count(*) INTO lifecycle_index_count
    FROM pg_catalog.pg_indexes
   WHERE schemaname = 'internal_rpc_authority'
     AND indexname IN (
       'authority_identity_one_current',
       'authority_identity_one_next',
       'authority_identity_one_previous'
     )
     AND indexdef LIKE '%UNIQUE INDEX%'
     AND indexdef LIKE '%credential_status%';
  IF lifecycle_index_count <> 3 THEN
    RAISE EXCEPTION 'bounded CURRENT NEXT PREVIOUS indexes are missing';
  END IF;
  SELECT count(*) INTO lifecycle_index_count
    FROM pg_catalog.pg_indexes
   WHERE schemaname = 'internal_rpc_authority'
     AND indexname IN (
       'authority_runtime_identity_one_current',
       'authority_runtime_identity_one_next',
       'authority_runtime_identity_one_previous'
     )
     AND indexdef LIKE '%UNIQUE INDEX%'
     AND indexdef LIKE '%credential_status%';
  IF lifecycle_index_count <> 3 THEN
    RAISE EXCEPTION 'runtime identity lifecycle indexes are missing';
  END IF;

  SELECT count(*) INTO function_count
    FROM pg_catalog.pg_proc AS function
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = function.pronamespace
   WHERE namespace.nspname = 'internal_rpc_authority'
     AND function.proname = 'is_active_runtime_database_session'
     AND function.prosecdef
     AND pg_catalog.pg_get_userbyid(function.proowner) =
       'internal_rpc_authority_readback_owner'
     AND function.proconfig @> ARRAY[
       'search_path=pg_catalog, internal_rpc_authority, pg_temp'
     ]
     AND pg_catalog.pg_get_function_result(function.oid) = 'boolean'
     AND pg_catalog.pg_get_function_identity_arguments(function.oid) =
       'p_capability_role text';
  IF function_count <> 1 THEN
    RAISE EXCEPTION 'runtime identity function signature or boundary mismatch';
  END IF;

  SELECT count(*) INTO function_count
    FROM pg_catalog.pg_proc AS function
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = function.pronamespace
   WHERE namespace.nspname = 'internal_rpc_authority'
     AND function.proname IN (
       'record_authority_key_delivery_readback',
       'record_authority_snapshot_readback'
     )
     AND function.prosecdef
     AND pg_catalog.pg_get_userbyid(function.proowner) =
       'internal_rpc_authority_readback_owner'
     AND function.proconfig @> ARRAY[
       'search_path=pg_catalog, internal_rpc_authority, pg_temp'
     ]
     AND pg_catalog.pg_get_function_result(function.oid) = 'bigint'
     AND pg_catalog.pg_get_function_identity_arguments(function.oid) =
       'p_attestation_receipt_id uuid';
  IF function_count <> 2 THEN
    RAISE EXCEPTION 'readback function signature or security boundary mismatch';
  END IF;
  SELECT count(*) INTO function_count
    FROM pg_catalog.pg_proc AS function
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = function.pronamespace
   WHERE namespace.nspname = 'internal_rpc_authority'
     AND function.proname IN (
       'record_authority_key_delivery_readback',
       'record_authority_snapshot_readback'
     );
  IF function_count <> 2 THEN
    RAISE EXCEPTION 'unsafe readback function overload exists';
  END IF;
  SELECT count(*) INTO function_count
    FROM pg_catalog.pg_proc AS function
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = function.pronamespace
   WHERE namespace.nspname = 'internal_rpc_authority'
     AND function.proname =
       'issue_authority_readback_attestation_challenge'
     AND function.prosecdef
     AND pg_catalog.pg_get_userbyid(function.proowner) =
       'internal_rpc_authority_readback_owner'
     AND function.proconfig @> ARRAY[
       'search_path=pg_catalog, internal_rpc_authority, pg_temp'
     ]
     AND pg_catalog.pg_get_function_result(function.oid) = 'uuid'
     AND pg_catalog.pg_get_function_identity_arguments(function.oid) =
       'p_intent_id uuid, p_challenge_id uuid, p_challenge_jti uuid, p_challenge_nonce text, p_challenge_digest_sha256 text, p_readback_credential_jti uuid, p_readback_credential_digest_sha256 text, p_idempotency_key uuid, p_semantic_request_digest_sha256 text';
  IF function_count <> 1 THEN
    RAISE EXCEPTION 'challenge issue function signature or boundary mismatch';
  END IF;
  SELECT count(*) INTO trigger_count
    FROM pg_catalog.pg_trigger AS trigger
    JOIN pg_catalog.pg_class AS relation
      ON relation.oid = trigger.tgrelid
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = relation.relnamespace
   WHERE namespace.nspname = 'internal_rpc_authority'
     AND relation.relname = 'authority_readback_attestation_receipts'
     AND trigger.tgname = 'authority_readback_challenge_consume'
     AND NOT trigger.tgisinternal;
  IF trigger_count <> 1 THEN
    RAISE EXCEPTION 'atomic readback challenge consume trigger is missing';
  END IF;
  SELECT count(*) INTO function_count
    FROM pg_catalog.pg_proc AS function
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = function.pronamespace
   WHERE namespace.nspname = 'internal_rpc_authority'
     AND function.proname =
       'consume_authority_readback_attestation_challenge'
     AND function.prosecdef
     AND pg_catalog.pg_get_userbyid(function.proowner) =
       'internal_rpc_authority_readback_owner'
     AND function.proconfig @> ARRAY[
       'search_path=pg_catalog, internal_rpc_authority, pg_temp'
     ]
     AND pg_catalog.pg_get_function_result(function.oid) = 'uuid'
     AND pg_catalog.pg_get_function_identity_arguments(function.oid) =
       'p_challenge_id uuid, p_receipt_id uuid, p_evidence_jti uuid, p_evidence_digest_sha256 text, p_verifier_generation bigint, p_idempotency_key uuid, p_semantic_request_digest_sha256 text';
  IF function_count <> 1 THEN
    RAISE EXCEPTION 'challenge consume function signature or boundary mismatch';
  END IF;
  SELECT count(*) INTO function_count
    FROM pg_catalog.pg_proc AS function
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = function.pronamespace
   WHERE namespace.nspname = 'internal_rpc_authority'
     AND function.proname IN (
       'issue_authority_readback_attestation_challenge',
       'consume_authority_readback_attestation_challenge'
     );
  IF function_count <> 2 THEN
    RAISE EXCEPTION 'unsafe challenge function overload exists';
  END IF;
END
$assertion$;

DO $assertion$
BEGIN
  IF NOT pg_catalog.has_function_privilege(
    'ira_control_plane_verifier_g1',
    'internal_rpc_authority.record_authority_key_delivery_readback(uuid)',
    'EXECUTE'
  ) OR NOT pg_catalog.has_function_privilege(
    'ira_control_plane_verifier_g2',
    'internal_rpc_authority.record_authority_snapshot_readback(uuid)',
    'EXECUTE'
  ) THEN
    RAISE EXCEPTION 'CURRENT or NEXT principal function grant is missing';
  END IF;
  IF pg_catalog.has_function_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.record_authority_key_delivery_readback(uuid)',
    'EXECUTE'
  ) OR pg_catalog.has_function_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.record_authority_snapshot_readback(uuid)',
    'EXECUTE'
  ) THEN
    RAISE EXCEPTION 'publisher can execute consumer readback function';
  END IF;
  IF NOT pg_catalog.has_function_privilege(
    'internal_rpc_authority_readback_attestor',
    'internal_rpc_authority.issue_authority_readback_attestation_challenge(uuid,uuid,uuid,text,text,uuid,text,uuid,text)',
    'EXECUTE'
  ) OR NOT pg_catalog.has_function_privilege(
    'internal_rpc_authority_readback_attestor',
    'internal_rpc_authority.consume_authority_readback_attestation_challenge(uuid,uuid,uuid,text,bigint,uuid,text)',
    'EXECUTE'
  ) OR pg_catalog.has_table_privilege(
    'internal_rpc_authority_readback_attestor',
    'internal_rpc_authority.authority_readback_attestation_challenges',
    'INSERT,UPDATE'
  ) OR pg_catalog.has_table_privilege(
    'internal_rpc_authority_readback_attestor',
    'internal_rpc_authority.authority_readback_attestation_receipts',
    'INSERT,UPDATE'
  ) THEN
    RAISE EXCEPTION 'attestor atomic challenge consume boundary is not exact';
  END IF;
  IF pg_catalog.has_table_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.authority_readback_attestation_receipts',
    'INSERT,UPDATE'
  ) OR pg_catalog.has_table_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.authority_snapshot_readbacks',
    'INSERT,UPDATE'
  ) OR pg_catalog.has_table_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.authority_key_delivery_readbacks',
    'INSERT,UPDATE'
  ) THEN
    RAISE EXCEPTION 'publisher can create receipt or write readback evidence';
  END IF;
  IF NOT pg_catalog.has_table_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.authority_snapshot_readbacks',
    'SELECT'
  ) OR NOT pg_catalog.has_table_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.authority_key_delivery_readbacks',
    'SELECT'
  ) THEN
    RAISE EXCEPTION 'publisher cannot read promotion evidence';
  END IF;
  IF pg_catalog.has_table_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.authority_runtime_database_identities',
    'SELECT,INSERT,UPDATE,DELETE'
  ) OR pg_catalog.has_table_privilege(
    'internal_rpc_authority_readback_attestor',
    'internal_rpc_authority.authority_runtime_database_identities',
    'SELECT,INSERT,UPDATE,DELETE'
  ) THEN
    RAISE EXCEPTION 'runtime role can mutate its server-side lifecycle fence';
  END IF;
  IF NOT pg_catalog.has_function_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.is_active_runtime_database_session(text)',
    'EXECUTE'
  ) OR NOT pg_catalog.has_function_privilege(
    'internal_rpc_authority_readback_attestor',
    'internal_rpc_authority.is_active_runtime_database_session(text)',
    'EXECUTE'
  ) OR EXISTS (
    SELECT 1
      FROM pg_catalog.pg_proc AS function
      JOIN pg_catalog.pg_namespace AS namespace
        ON namespace.oid = function.pronamespace
      CROSS JOIN LATERAL pg_catalog.aclexplode(
        COALESCE(
          function.proacl,
          pg_catalog.acldefault('f', function.proowner)
        )
      ) AS privilege
     WHERE namespace.nspname = 'internal_rpc_authority'
       AND function.proname = 'is_active_runtime_database_session'
       AND privilege.grantee = 0
       AND privilege.privilege_type = 'EXECUTE'
  ) THEN
    RAISE EXCEPTION 'runtime session fence execute boundary is not exact';
  END IF;
END
$assertion$;
