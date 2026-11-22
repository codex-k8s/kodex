-- Envelope только фиксированного canonical CP051 readback; business data и effects не добавляются.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='15s';SET LOCAL lock_timeout='500ms';SET LOCAL search_path=pg_catalog;
WITH paired_canonical(value) AS MATERIALIZED (
/*__PAIRED_CONTROL_PLANE_CANONICAL__*/
)
SELECT pg_catalog.json_build_object(
 'profile','PAIRED_TOKEN_CP051','readOnly',pg_catalog.current_setting('transaction_read_only'),
 'isolation',pg_catalog.current_setting('transaction_isolation'),
 'backend',(SELECT pg_catalog.json_build_object('systemIdentifier',system_identifier::pg_catalog.text,
  'databaseOID',oid::pg_catalog.text,'database',pg_catalog.current_database(),
  'port',pg_catalog.current_setting('port'),'serverAddress',pg_catalog.inet_server_addr(),'serverPort',pg_catalog.inet_server_port())
  FROM pg_catalog.pg_control_system(),pg_catalog.pg_database WHERE datname=pg_catalog.current_database()),
 'proof',value::pg_catalog.jsonb) FROM paired_canonical;
ROLLBACK;
