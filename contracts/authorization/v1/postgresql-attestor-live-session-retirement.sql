\set ON_ERROR_STOP on

SET ROLE internal_rpc_authority_readback_attestor;
SELECT pg_catalog.pg_advisory_lock(186201);
SELECT pg_catalog.pg_sleep(5);

BEGIN;
DO $expected_rejection$
BEGIN
  BEGIN
    PERFORM internal_rpc_authority.issue_authority_readback_attestation_challenge(
      '84000000-0000-4000-8000-000000000001',
      '7d000000-0000-4000-8000-000000000001',
      '7e000000-0000-4000-8000-000000000001',
      'GGGGGGGGGGGGGGGGGGGGGG',
      repeat('6', 64),
      '74000000-0000-4000-8000-000000000001',
      repeat('0', 64),
      '7f000000-0000-4000-8000-000000000001',
      repeat('7', 64)
    );
    RAISE EXCEPTION 'retired open attestor session issued a challenge';
  EXCEPTION
    WHEN raise_exception THEN
      IF SQLERRM = 'retired open attestor session issued a challenge' THEN
        RAISE;
      END IF;
      IF SQLERRM <> 'readback attestor runtime database identity rejected' THEN
        RAISE;
      END IF;
  END;

  IF EXISTS (
    SELECT 1
      FROM internal_rpc_authority.authority_readback_intents
  ) OR EXISTS (
    SELECT 1
      FROM internal_rpc_authority.authority_readback_attestation_challenges
  ) OR EXISTS (
    SELECT 1
      FROM internal_rpc_authority.authority_readback_attestation_receipts
  ) THEN
    RAISE EXCEPTION 'retired open attestor session retained protected reads';
  END IF;
END
$expected_rejection$;
ROLLBACK;

SELECT pg_catalog.pg_advisory_unlock(186201);
RESET ROLE;
