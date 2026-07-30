\set ON_ERROR_STOP on

SET ROLE internal_rpc_authority_publisher;
SELECT pg_catalog.pg_advisory_lock(186200);
SELECT pg_catalog.pg_sleep(5);

BEGIN TRANSACTION ISOLATION LEVEL SERIALIZABLE;
DO $expected_rejection$
BEGIN
  BEGIN
    PERFORM internal_rpc_authority.promote_authority_workload_database_identity(
      'control-plane',
      'AUTHORIZATION_VERIFIER',
      1,
      2,
      '83000000-0000-4000-8000-000000000001',
      '84000000-0000-4000-8000-000000000001'
    );
    RAISE EXCEPTION 'retired open publisher session promoted credentials';
  EXCEPTION
    WHEN raise_exception THEN
      IF SQLERRM = 'retired open publisher session promoted credentials' THEN
        RAISE;
      END IF;
      IF SQLERRM <> 'publisher runtime database identity rejected' THEN
        RAISE;
      END IF;
  END;

  IF EXISTS (
    SELECT 1
      FROM internal_rpc_authority.authority_key_delivery_readbacks
  ) OR EXISTS (
    SELECT 1
      FROM internal_rpc_authority.authority_snapshot_readbacks
  ) THEN
    RAISE EXCEPTION 'retired open publisher session retained evidence read';
  END IF;
END
$expected_rejection$;

ROLLBACK;
SELECT pg_catalog.pg_advisory_unlock(186200);
RESET ROLE;
