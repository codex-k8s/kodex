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
  IF pg_catalog.has_schema_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority',
    'CREATE'
  ) OR pg_catalog.has_schema_privilege(
    'ira_readback_attestor_g1',
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
    'ira_readback_attestor_g1',
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
BEGIN
  SELECT count(*) INTO invalid_table_count
    FROM pg_catalog.pg_class AS relation
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = relation.relnamespace
   WHERE namespace.nspname = 'internal_rpc_authority'
     AND relation.relname IN (
       'authority_readback_intents',
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
END
$assertion$;
