\set ON_ERROR_STOP on

DO $assertion$
DECLARE
  owner_row pg_catalog.pg_roles%ROWTYPE;
BEGIN
  SELECT * INTO STRICT owner_row
    FROM pg_catalog.pg_roles
   WHERE rolname = 'internal_rpc_authority_readback_owner';
  IF owner_row.rolcanlogin OR owner_row.rolsuper OR owner_row.rolbypassrls
     OR owner_row.rolcreatedb OR owner_row.rolcreaterole OR owner_row.rolinherit THEN
    RAISE EXCEPTION 'readback owner privileges are not minimal';
  END IF;
  IF pg_catalog.has_schema_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority',
    'CREATE'
  ) THEN
    RAISE EXCEPTION 'publisher can create objects in protected schema';
  END IF;
  IF pg_catalog.pg_has_role(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority_readback_owner',
    'MEMBER'
  ) OR pg_catalog.pg_has_role(
    'ira_control_api_gateway_issuer_g1',
    'internal_rpc_authority_readback_owner',
    'MEMBER'
  ) OR pg_catalog.pg_has_role(
    'ira_control_plane_verifier_g1',
    'internal_rpc_authority_readback_owner',
    'MEMBER'
  ) OR pg_catalog.pg_has_role(
    'ira_control_plane_resolver_g1',
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
BEGIN
  SELECT count(*) INTO invalid_table_count
    FROM pg_catalog.pg_class AS relation
    JOIN pg_catalog.pg_namespace AS namespace
      ON namespace.oid = relation.relnamespace
   WHERE namespace.nspname = 'internal_rpc_authority'
     AND relation.relname IN (
       'authority_key_delivery_readbacks',
       'authority_snapshot_readbacks'
     )
     AND (NOT relation.relrowsecurity OR NOT relation.relforcerowsecurity);
  IF invalid_table_count <> 0 THEN
    RAISE EXCEPTION 'readback tables do not force RLS';
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
     AND (
       (
         function.proname = 'record_authority_key_delivery_readback'
         AND pg_catalog.pg_get_function_identity_arguments(function.oid) =
           'p_source_revision bigint, p_digest_sha256 text, p_key_generation bigint, p_signer_generation bigint, p_served_proof_sha256 text'
       ) OR (
         function.proname = 'record_authority_snapshot_readback'
         AND pg_catalog.pg_get_function_identity_arguments(function.oid) =
           'p_source_revision bigint, p_digest_sha256 text, p_key_set_revision bigint, p_policy_revision bigint, p_signer_generation bigint, p_served_proof_sha256 text'
       )
     );
  IF function_count <> 2 THEN
    RAISE EXCEPTION 'readback function owner, security or search_path mismatch';
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
    'internal_rpc_authority.record_authority_key_delivery_readback(bigint,text,bigint,bigint,text)',
    'EXECUTE'
  ) OR NOT pg_catalog.has_function_privilege(
    'ira_control_plane_resolver_g1',
    'internal_rpc_authority.record_authority_snapshot_readback(bigint,text,bigint,bigint,bigint,text)',
    'EXECUTE'
  ) THEN
    RAISE EXCEPTION 'exact workload-role function grant is missing';
  END IF;
  IF pg_catalog.has_function_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.record_authority_key_delivery_readback(bigint,text,bigint,bigint,text)',
    'EXECUTE'
  ) OR pg_catalog.has_function_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.record_authority_snapshot_readback(bigint,text,bigint,bigint,bigint,text)',
    'EXECUTE'
  ) THEN
    RAISE EXCEPTION 'publisher can execute readback function';
  END IF;
  IF pg_catalog.has_table_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.authority_snapshot_readbacks',
    'INSERT,UPDATE'
  ) OR pg_catalog.has_table_privilege(
    'internal_rpc_authority_publisher',
    'internal_rpc_authority.authority_key_delivery_readbacks',
    'INSERT,UPDATE'
  ) THEN
    RAISE EXCEPTION 'publisher can directly write readback table';
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
