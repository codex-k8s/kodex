BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='10s';
SET LOCAL log_statement='none';
SET LOCAL log_min_error_statement='panic';
SET LOCAL log_parameter_max_length=0;
SET LOCAL log_parameter_max_length_on_error=0;
SET LOCAL password_encryption='scram-sha-256';
SELECT pg_advisory_xact_lock(hashtextextended('kodex.authority.artifact-retention-issuer.initial-password',0));
DO $artifact_issuer_login$
DECLARE existing_password boolean;
BEGIN
 IF current_database()<>'internal_rpc_authority' OR session_user<>'postgres'
    OR inet_server_addr() IS NOT NULL OR inet_server_port() IS NOT NULL
    OR current_setting('port')<>'5432'
    OR (SELECT system_identifier::text FROM pg_catalog.pg_control_system()) IS DISTINCT FROM '@SYSTEM_IDENTIFIER@'
    OR (SELECT oid::text FROM pg_catalog.pg_database WHERE datname=current_database()) IS DISTINCT FROM '@DATABASE_OID@'
    OR (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied) IS DISTINCT FROM 20261115000200::bigint
    OR NOT EXISTS(SELECT 1 FROM internal_rpc_authority.authority_workload_database_identities
      WHERE principal='ira_artifact_retention_issuer_g1' AND workload_id='artifact-retention'
        AND generation=1 AND capability='ISSUER' AND lifecycle_status='CURRENT') THEN
  RAISE EXCEPTION 'Artifact-retention issuer login origin is not ready';
 END IF;
 SELECT login.rolpassword IS NOT NULL INTO existing_password
 FROM pg_catalog.pg_authid login
 WHERE login.rolname='ira_artifact_retention_issuer_g1' AND login.rolcanlogin
   AND NOT login.rolsuper AND NOT login.rolinherit AND NOT login.rolcreatedb AND NOT login.rolcreaterole
   AND NOT login.rolreplication AND NOT login.rolbypassrls
 FOR UPDATE;
 IF NOT FOUND OR existing_password IS DISTINCT FROM false THEN
  RAISE EXCEPTION 'Artifact-retention issuer login already has credentials or is not registered';
 END IF;
END $artifact_issuer_login$;
