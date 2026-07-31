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

  BEGIN
    PERFORM internal_rpc_authority.publisher_append_snapshot_history(
      2,
      repeat('a', 64),
      2,
      2,
      2,
      1,
      repeat('b', 64),
      'header.payload.signature'
    );
    RAISE EXCEPTION 'retired open publisher session appended snapshot history';
  EXCEPTION
    WHEN raise_exception THEN
      IF SQLERRM =
        'retired open publisher session appended snapshot history' THEN
        RAISE;
      END IF;
      IF SQLERRM <> 'publisher runtime database identity rejected' THEN
        RAISE;
      END IF;
  END;

  BEGIN
    PERFORM internal_rpc_authority.publisher_record_rotation_intent(
      '85000000-0000-4000-8000-000000000001',
      2,
      repeat('c', 64),
      2,
      '86000000-0000-4000-8000-000000000001'
    );
    RAISE EXCEPTION 'retired open publisher session recorded rotation intent';
  EXCEPTION
    WHEN raise_exception THEN
      IF SQLERRM =
        'retired open publisher session recorded rotation intent' THEN
        RAISE;
      END IF;
      IF SQLERRM <> 'publisher runtime database identity rejected' THEN
        RAISE;
      END IF;
  END;

  BEGIN
    PERFORM * FROM internal_rpc_authority.publisher_read_restore_fence();
    RAISE EXCEPTION 'retired open publisher session read restore fence';
  EXCEPTION
    WHEN raise_exception THEN
      IF SQLERRM = 'retired open publisher session read restore fence' THEN
        RAISE;
      END IF;
      IF SQLERRM <> 'publisher runtime database identity rejected' THEN
        RAISE;
      END IF;
  END;

  BEGIN
    PERFORM 1 FROM internal_rpc_authority.authority_snapshot_history;
    RAISE EXCEPTION 'retired publisher retained direct snapshot read';
  EXCEPTION
    WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    INSERT INTO internal_rpc_authority.authority_rotation_intents (
      intent_id, source_revision, canonical_digest_sha256,
      target_signer_generation, lifecycle_status, idempotency_key
    ) VALUES (
      '87000000-0000-4000-8000-000000000001',
      3,
      repeat('d', 64),
      3,
      'PREPARED',
      '88000000-0000-4000-8000-000000000001'
    );
    RAISE EXCEPTION 'retired publisher retained direct rotation insert';
  EXCEPTION
    WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    PERFORM 1 FROM internal_rpc_authority.authority_restore_fences;
    RAISE EXCEPTION 'retired publisher retained direct restore fence read';
  EXCEPTION
    WHEN insufficient_privilege THEN NULL;
  END;
END
$expected_rejection$;

ROLLBACK;
SELECT pg_catalog.pg_advisory_unlock(186200);
RESET ROLE;
