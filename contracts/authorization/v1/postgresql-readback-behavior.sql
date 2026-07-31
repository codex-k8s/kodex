\set ON_ERROR_STOP on
\getenv admin_dsn INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_DSN
\getenv attestor_dsn INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_ATTESTOR_DSN
\getenv attestor_g2_dsn INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_ATTESTOR_G2_DSN
\getenv publisher_dsn INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_PUBLISHER_DSN
\getenv verifier_g1_dsn INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_VERIFIER_G1_DSN
\getenv verifier_g2_dsn INTERNAL_RPC_AUTHORITY_CONTRACT_POSTGRES_VERIFIER_G2_DSN

INSERT INTO internal_rpc_authority.authority_runtime_database_identities (
  session_login,
  capability_role,
  credential_generation,
  credential_status
) VALUES
  ('ira_publisher_g1', 'PUBLISHER', 1, 'CURRENT'),
  ('ira_publisher_g2', 'PUBLISHER', 2, 'NEXT'),
  ('ira_readback_attestor_g1', 'READBACK_ATTESTOR', 1, 'CURRENT'),
  ('ira_readback_attestor_g2', 'READBACK_ATTESTOR', 2, 'NEXT');

INSERT INTO internal_rpc_authority.authority_workload_database_identities (
  session_login,
  workload_id,
  workload_spiffe_id,
  role,
  workload_generation,
  credential_generation,
  credential_status
) VALUES
  (
    'ira_control_plane_verifier_g1',
    'control-plane',
    'spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    1,
    'CURRENT'
  ),
  (
    'ira_control_plane_verifier_g2',
    'control-plane',
    'spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane',
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
  workload_spiffe_id,
  role,
  workload_generation,
  credential_generation,
  material_generation,
  readback_purpose,
  readback_credential_jti,
  readback_credential_digest_sha256,
  possession_key_kid,
  possession_key_generation,
  possession_key_thumbprint_sha256,
  readback_credential_signer_generation,
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
    'spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    1,
    1,
    'KEY_DELIVERY_READBACK',
    '71000000-0000-4000-8000-000000000001',
    repeat('5', 64),
    'readback-possession-g1',
    1,
    repeat('6', 64),
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
    'spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    1,
    1,
    'SNAPSHOT_READBACK',
    '72000000-0000-4000-8000-000000000001',
    repeat('7', 64),
    'readback-possession-g1',
    1,
    repeat('6', 64),
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
    'spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    2,
    2,
    'KEY_DELIVERY_READBACK',
    '73000000-0000-4000-8000-000000000001',
    repeat('8', 64),
    'readback-possession-g2',
    2,
    repeat('9', 64),
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
    'spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane',
    'AUTHORIZATION_VERIFIER',
    1,
    2,
    2,
    'SNAPSHOT_READBACK',
    '74000000-0000-4000-8000-000000000001',
    repeat('0', 64),
    'readback-possession-g2',
    2,
    repeat('9', 64),
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

\connect :attestor_dsn
BEGIN;
SET LOCAL ROLE internal_rpc_authority_readback_attestor;
DO $expected_rejection$
BEGIN
  BEGIN
    INSERT INTO internal_rpc_authority.authority_readback_attestation_challenges
      DEFAULT VALUES;
    RAISE EXCEPTION 'attestor direct challenge write was accepted';
  EXCEPTION
    WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    INSERT INTO internal_rpc_authority.authority_readback_attestation_receipts
      DEFAULT VALUES;
    RAISE EXCEPTION 'attestor direct receipt write was accepted';
  EXCEPTION
    WHEN insufficient_privilege THEN NULL;
  END;
END
$expected_rejection$;

SELECT internal_rpc_authority.issue_authority_readback_attestation_challenge(
  '81000000-0000-4000-8000-000000000001',
  '61000000-0000-4000-8000-000000000001',
  '62000000-0000-4000-8000-000000000001',
  'AAAAAAAAAAAAAAAAAAAAAA',
  repeat('a', 64),
  '71000000-0000-4000-8000-000000000001',
  repeat('5', 64),
  '63000000-0000-4000-8000-000000000001',
  repeat('1', 64)
);
SELECT internal_rpc_authority.issue_authority_readback_attestation_challenge(
  '82000000-0000-4000-8000-000000000001',
  '64000000-0000-4000-8000-000000000001',
  '65000000-0000-4000-8000-000000000001',
  'BBBBBBBBBBBBBBBBBBBBBB',
  repeat('b', 64),
  '72000000-0000-4000-8000-000000000001',
  repeat('7', 64),
  '66000000-0000-4000-8000-000000000001',
  repeat('2', 64)
);
SELECT internal_rpc_authority.issue_authority_readback_attestation_challenge(
  '83000000-0000-4000-8000-000000000001',
  '67000000-0000-4000-8000-000000000001',
  '68000000-0000-4000-8000-000000000001',
  'CCCCCCCCCCCCCCCCCCCCCC',
  repeat('c', 64),
  '73000000-0000-4000-8000-000000000001',
  repeat('8', 64),
  '69000000-0000-4000-8000-000000000001',
  repeat('3', 64)
);
SELECT internal_rpc_authority.issue_authority_readback_attestation_challenge(
  '84000000-0000-4000-8000-000000000001',
  '6a000000-0000-4000-8000-000000000001',
  '6b000000-0000-4000-8000-000000000001',
  'DDDDDDDDDDDDDDDDDDDDDD',
  repeat('d', 64),
  '74000000-0000-4000-8000-000000000001',
  repeat('0', 64),
  '6c000000-0000-4000-8000-000000000001',
  repeat('4', 64)
);
-- Lost-response retry возвращает тот же persisted challenge.
SELECT internal_rpc_authority.issue_authority_readback_attestation_challenge(
  '84000000-0000-4000-8000-000000000001',
  'ffffffff-ffff-4fff-8fff-ffffffffffff',
  'eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee',
  'EEEEEEEEEEEEEEEEEEEEEE',
  repeat('e', 64),
  '74000000-0000-4000-8000-000000000001',
  repeat('0', 64),
  '6c000000-0000-4000-8000-000000000001',
  repeat('4', 64)
);

SELECT internal_rpc_authority.consume_authority_readback_attestation_challenge(
  '61000000-0000-4000-8000-000000000001',
  '91000000-0000-4000-8000-000000000001',
  'a1000000-0000-4000-8000-000000000001',
  repeat('a', 64),
  1,
  '51000000-0000-4000-8000-000000000001',
  repeat('a', 64)
);
SELECT internal_rpc_authority.consume_authority_readback_attestation_challenge(
  '64000000-0000-4000-8000-000000000001',
  '92000000-0000-4000-8000-000000000001',
  'a2000000-0000-4000-8000-000000000001',
  repeat('c', 64),
  1,
  '52000000-0000-4000-8000-000000000001',
  repeat('b', 64)
);
SELECT internal_rpc_authority.consume_authority_readback_attestation_challenge(
  '67000000-0000-4000-8000-000000000001',
  '93000000-0000-4000-8000-000000000001',
  'a3000000-0000-4000-8000-000000000001',
  repeat('e', 64),
  2,
  '53000000-0000-4000-8000-000000000001',
  repeat('c', 64)
);
SELECT internal_rpc_authority.consume_authority_readback_attestation_challenge(
  '6a000000-0000-4000-8000-000000000001',
  '94000000-0000-4000-8000-000000000001',
  'a4000000-0000-4000-8000-000000000001',
  repeat('0', 64),
  2,
  '54000000-0000-4000-8000-000000000001',
  repeat('d', 64)
);
-- Lost-response retry возвращает тот же persisted receipt.
SELECT internal_rpc_authority.consume_authority_readback_attestation_challenge(
  '6a000000-0000-4000-8000-000000000001',
  'ffffffff-ffff-4fff-8fff-ffffffffffff',
  'a4000000-0000-4000-8000-000000000001',
  repeat('0', 64),
  2,
  '54000000-0000-4000-8000-000000000001',
  repeat('d', 64)
);
COMMIT;

\connect :attestor_g2_dsn
BEGIN;
SET LOCAL ROLE internal_rpc_authority_readback_attestor;
SELECT internal_rpc_authority.issue_authority_readback_attestation_challenge(
  '84000000-0000-4000-8000-000000000001',
  '6d000000-0000-4000-8000-000000000001',
  '6e000000-0000-4000-8000-000000000001',
  'FFFFFFFFFFFFFFFFFFFFFF',
  repeat('f', 64),
  '74000000-0000-4000-8000-000000000001',
  repeat('0', 64),
  '6f000000-0000-4000-8000-000000000001',
  repeat('5', 64)
);
SELECT internal_rpc_authority.consume_authority_readback_attestation_challenge(
  '6d000000-0000-4000-8000-000000000001',
  '95000000-0000-4000-8000-000000000001',
  'a5000000-0000-4000-8000-000000000001',
  repeat('1', 64),
  2,
  '55000000-0000-4000-8000-000000000001',
  repeat('e', 64)
);
COMMIT;

\connect :verifier_g1_dsn
SELECT internal_rpc_authority.record_authority_key_delivery_readback(
  '91000000-0000-4000-8000-000000000001'
);
SELECT internal_rpc_authority.record_authority_snapshot_readback(
  '92000000-0000-4000-8000-000000000001'
);

\connect :verifier_g2_dsn
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

\connect :publisher_dsn
BEGIN TRANSACTION ISOLATION LEVEL SERIALIZABLE;
SET LOCAL ROLE internal_rpc_authority_publisher;
DO $expected_rejection$
BEGIN
  BEGIN
    INSERT INTO internal_rpc_authority.authority_key_delivery_readbacks
      DEFAULT VALUES;
    RAISE EXCEPTION 'publisher direct key delivery write was accepted';
  EXCEPTION
    WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    UPDATE internal_rpc_authority.authority_snapshot_readbacks
       SET recorded_at = recorded_at
     WHERE false;
    RAISE EXCEPTION 'publisher direct snapshot update was accepted';
  EXCEPTION
    WHEN insufficient_privilege THEN NULL;
  END;
  BEGIN
    PERFORM internal_rpc_authority.record_authority_snapshot_readback(
      '00000000-0000-4000-8000-000000000000'
    );
    RAISE EXCEPTION 'publisher consumer readback function was accepted';
  EXCEPTION
    WHEN insufficient_privilege THEN NULL;
  END;
END
$expected_rejection$;
SELECT internal_rpc_authority.promote_authority_workload_database_identity(
  'control-plane',
  'AUTHORIZATION_VERIFIER',
  1,
  2,
  '83000000-0000-4000-8000-000000000001',
  '84000000-0000-4000-8000-000000000001'
);
COMMIT;

\connect :admin_dsn
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

\connect :verifier_g1_dsn
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
\connect :admin_dsn
