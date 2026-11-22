-- Только текущее состояние владельца authority; никаких status Job либо DDL.
BEGIN READ ONLY;
SET LOCAL statement_timeout='5s';
SET LOCAL lock_timeout='1s';
SELECT pg_catalog.json_build_object(
 'database',pg_catalog.current_database(),
 'readOnly',pg_catalog.current_setting('transaction_read_only'),
 'gooseVersion',(SELECT max(version_id) FROM public.goose_db_version WHERE is_applied));
ROLLBACK;
