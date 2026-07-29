\set ON_ERROR_STOP on

INSERT INTO internal_rpc_authority.authority_workload_database_identities (
  session_login,
  workload_id,
  role,
  workload_generation,
  credential_generation,
  credential_status
) VALUES
  (
    'ira_control_plane_verifier_g1',
    'control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    1,
    'CURRENT'
  ),
  (
    'ira_control_plane_verifier_g2',
    'control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    2,
    'NEXT'
  );

INSERT INTO internal_rpc_authority.authority_readback_intents (
  intent_id,
  intent_kind,
  intent_revision,
  workload_id,
  role,
  workload_generation,
  credential_generation,
  source_revision,
  digest_sha256,
  key_generation,
  key_set_revision,
  policy_revision,
  signer_generation,
  intent_status,
  pinned_at
) VALUES
  (
    '81000000-0000-4000-8000-000000000001',
    'KEY_DELIVERY',
    1,
    'control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    1,
    1,
    repeat('1', 64),
    1,
    NULL,
    NULL,
    1,
    'PINNED',
    pg_catalog.clock_timestamp()
  ),
  (
    '82000000-0000-4000-8000-000000000001',
    'SNAPSHOT',
    2,
    'control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    1,
    1,
    repeat('2', 64),
    NULL,
    1,
    1,
    1,
    'PINNED',
    pg_catalog.clock_timestamp()
  ),
  (
    '83000000-0000-4000-8000-000000000001',
    'KEY_DELIVERY',
    3,
    'control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    2,
    2,
    repeat('3', 64),
    2,
    NULL,
    NULL,
    2,
    'PINNED',
    pg_catalog.clock_timestamp()
  ),
  (
    '84000000-0000-4000-8000-000000000001',
    'SNAPSHOT',
    4,
    'control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    2,
    2,
    repeat('4', 64),
    NULL,
    2,
    2,
    2,
    'PINNED',
    pg_catalog.clock_timestamp()
  );

INSERT INTO internal_rpc_authority.authority_readback_attestation_receipts (
  receipt_id,
  intent_id,
  session_login,
  workload_id,
  role,
  workload_generation,
  credential_generation,
  evidence_jti,
  evidence_digest_sha256,
  served_state_digest_sha256,
  public_key_thumbprint_sha256,
  verifier_generation,
  verification_method,
  verified_at,
  expires_at
) VALUES
  (
    '91000000-0000-4000-8000-000000000001',
    '81000000-0000-4000-8000-000000000001',
    'ira_control_plane_verifier_g1',
    'control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    1,
    'a1000000-0000-4000-8000-000000000001',
    repeat('a', 64),
    repeat('1', 64),
    repeat('b', 64),
    1,
    'ES256_ROLE_BOUND_SERVED_STATE_CHALLENGE_V1',
    pg_catalog.clock_timestamp(),
    pg_catalog.clock_timestamp() + interval '5 minutes'
  ),
  (
    '92000000-0000-4000-8000-000000000001',
    '82000000-0000-4000-8000-000000000001',
    'ira_control_plane_verifier_g1',
    'control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    1,
    'a2000000-0000-4000-8000-000000000001',
    repeat('c', 64),
    repeat('2', 64),
    repeat('d', 64),
    1,
    'ES256_ROLE_BOUND_SERVED_STATE_CHALLENGE_V1',
    pg_catalog.clock_timestamp(),
    pg_catalog.clock_timestamp() + interval '5 minutes'
  ),
  (
    '93000000-0000-4000-8000-000000000001',
    '83000000-0000-4000-8000-000000000001',
    'ira_control_plane_verifier_g2',
    'control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    2,
    'a3000000-0000-4000-8000-000000000001',
    repeat('e', 64),
    repeat('3', 64),
    repeat('f', 64),
    2,
    'ES256_ROLE_BOUND_SERVED_STATE_CHALLENGE_V1',
    pg_catalog.clock_timestamp(),
    pg_catalog.clock_timestamp() + interval '5 minutes'
  ),
  (
    '94000000-0000-4000-8000-000000000001',
    '84000000-0000-4000-8000-000000000001',
    'ira_control_plane_verifier_g2',
    'control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    2,
    'a4000000-0000-4000-8000-000000000001',
    repeat('0', 64),
    repeat('4', 64),
    repeat('9', 64),
    2,
    'ES256_ROLE_BOUND_SERVED_STATE_CHALLENGE_V1',
    pg_catalog.clock_timestamp(),
    pg_catalog.clock_timestamp() + interval '5 minutes'
  );

SET SESSION AUTHORIZATION ira_control_plane_verifier_g1;
SELECT internal_rpc_authority.record_authority_key_delivery_readback(
  '91000000-0000-4000-8000-000000000001'
);
SELECT internal_rpc_authority.record_authority_snapshot_readback(
  '92000000-0000-4000-8000-000000000001'
);
RESET SESSION AUTHORIZATION;

SET SESSION AUTHORIZATION ira_control_plane_verifier_g2;
SELECT internal_rpc_authority.record_authority_key_delivery_readback(
  '93000000-0000-4000-8000-000000000001'
);
SELECT internal_rpc_authority.record_authority_snapshot_readback(
  '94000000-0000-4000-8000-000000000001'
);
DO $expected_rejection$
BEGIN
  PERFORM internal_rpc_authority.record_authority_snapshot_readback(
    '94000000-0000-4000-8000-000000000001'
  );
  RAISE EXCEPTION 'reused attestation receipt was accepted';
EXCEPTION
  WHEN unique_violation OR raise_exception THEN
    IF SQLERRM = 'reused attestation receipt was accepted' THEN
      RAISE;
    END IF;
END
$expected_rejection$;
RESET SESSION AUTHORIZATION;

BEGIN TRANSACTION ISOLATION LEVEL SERIALIZABLE;
SET LOCAL SESSION AUTHORIZATION internal_rpc_authority_publisher;
SELECT internal_rpc_authority.promote_authority_workload_database_identity(
  'control-plane',
  'AUTHORIZATION_VERIFIER',
  1,
  2,
  '83000000-0000-4000-8000-000000000001',
  '84000000-0000-4000-8000-000000000001'
);
COMMIT;

DO $assertion$
BEGIN
  IF NOT EXISTS (
    SELECT 1
      FROM internal_rpc_authority.authority_workload_database_identities
     WHERE session_login = 'ira_control_plane_verifier_g2'
       AND credential_status = 'CURRENT'
  ) OR NOT EXISTS (
    SELECT 1
      FROM internal_rpc_authority.authority_workload_database_identities
     WHERE session_login = 'ira_control_plane_verifier_g1'
       AND credential_status = 'PREVIOUS'
       AND overlap_not_after > pg_catalog.clock_timestamp()
  ) THEN
    RAISE EXCEPTION 'CURRENT NEXT promotion did not preserve bounded PREVIOUS overlap';
  END IF;
END
$assertion$;

UPDATE internal_rpc_authority.authority_workload_database_identities
   SET credential_status = 'RETIRED',
       retired_at = pg_catalog.clock_timestamp()
 WHERE session_login = 'ira_control_plane_verifier_g1';

SET SESSION AUTHORIZATION ira_control_plane_verifier_g1;
DO $expected_rejection$
BEGIN
  PERFORM internal_rpc_authority.record_authority_key_delivery_readback(
    '00000000-0000-4000-8000-000000000000'
  );
  RAISE EXCEPTION 'retired principal was accepted';
EXCEPTION
  WHEN raise_exception THEN
    IF SQLERRM = 'retired principal was accepted' THEN
      RAISE;
    END IF;
END
$expected_rejection$;
RESET SESSION AUTHORIZATION;
