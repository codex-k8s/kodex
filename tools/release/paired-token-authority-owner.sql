-- Identity, schema head и canonical owner status принадлежат одной local owner session и snapshot.
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout='5s';SET LOCAL lock_timeout='1s';
SET LOCAL idle_in_transaction_session_timeout='5s';SET LOCAL search_path=pg_catalog;
WITH paired_head(value) AS MATERIALIZED (
/*__PAIRED_AUTHORITY_HEAD__*/
), paired_state(value) AS MATERIALIZED (
/*__PAIRED_AUTHORITY_STATUS__*/
)
SELECT pg_catalog.json_build_object(
 'profile','PAIRED_TOKEN_AUTHORITY110','readOnly',pg_catalog.current_setting('transaction_read_only'),
 'isolation',pg_catalog.current_setting('transaction_isolation'),
 'backend',(SELECT pg_catalog.json_build_object('systemIdentifier',system_identifier::pg_catalog.text,
  'databaseOID',oid::pg_catalog.text,'database',pg_catalog.current_database(),
  'port',pg_catalog.current_setting('port'),'serverAddress',pg_catalog.inet_server_addr(),'serverPort',pg_catalog.inet_server_port())
  FROM pg_catalog.pg_control_system(),pg_catalog.pg_database WHERE datname=pg_catalog.current_database()),
 'head',paired_head.value,'state',paired_state.value) FROM paired_head CROSS JOIN paired_state;
ROLLBACK;
