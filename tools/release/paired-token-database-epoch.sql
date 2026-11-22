-- Финальная сверка обеих database после proof/status: Pod UID не заменяет OID epoch.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';SET LOCAL lock_timeout='1s';
SET LOCAL idle_in_transaction_session_timeout='5s';SET LOCAL search_path=pg_catalog;
SELECT pg_catalog.json_build_object(
 'profile','PAIRED_TOKEN_DATABASE_EPOCH','readOnly',pg_catalog.current_setting('transaction_read_only'),
 'isolation',pg_catalog.current_setting('transaction_isolation'),
 'systemIdentifier',system_identifier::pg_catalog.text,'database',pg_catalog.current_database(),
 'port',pg_catalog.current_setting('port'),'serverAddress',pg_catalog.inet_server_addr(),'serverPort',pg_catalog.inet_server_port(),
 'databases',(SELECT pg_catalog.json_object_agg(datname,oid::pg_catalog.text ORDER BY datname)
  FROM pg_catalog.pg_database WHERE datname IN ('control_plane','internal_rpc_authority')))
FROM pg_catalog.pg_control_system();
ROLLBACK;
