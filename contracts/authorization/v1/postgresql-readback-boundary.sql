BEGIN;

CREATE ROLE internal_rpc_authority_readback_owner
  NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE internal_rpc_authority_publisher
  NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE internal_rpc_authority_readback_attestor
  NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_publisher_g1
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_publisher_g2
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_readback_attestor_g1
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_readback_attestor_g2
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_control_api_gateway_issuer_g1
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_control_api_gateway_issuer_g2
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_control_plane_verifier_g1
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_control_plane_verifier_g2
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_control_plane_resolver_g1
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_control_plane_resolver_g2
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

GRANT internal_rpc_authority_publisher TO ira_publisher_g1, ira_publisher_g2;
GRANT internal_rpc_authority_readback_attestor
  TO ira_readback_attestor_g1, ira_readback_attestor_g2;

CREATE SCHEMA internal_rpc_authority
  AUTHORIZATION internal_rpc_authority_readback_owner;
REVOKE ALL ON SCHEMA internal_rpc_authority FROM PUBLIC;
GRANT USAGE ON SCHEMA internal_rpc_authority TO
  internal_rpc_authority_publisher,
  internal_rpc_authority_readback_attestor,
  ira_publisher_g1,
  ira_publisher_g2,
  ira_readback_attestor_g1,
  ira_readback_attestor_g2,
  ira_control_api_gateway_issuer_g1,
  ira_control_api_gateway_issuer_g2,
  ira_control_plane_verifier_g1,
  ira_control_plane_verifier_g2,
  ira_control_plane_resolver_g1,
  ira_control_plane_resolver_g2;

CREATE TABLE internal_rpc_authority.authority_workload_database_identities (
  session_login name PRIMARY KEY,
  workload_id text NOT NULL,
  workload_spiffe_id text NOT NULL
    CHECK (workload_spiffe_id ~ '^spiffe://mattercodex[.]local/'),
  role text NOT NULL CHECK (role IN (
    'AUTHORIZATION_ISSUER',
    'AUTHORIZATION_VERIFIER',
    'AUTHORITY_PROOF_RESOLVER'
  )),
  workload_generation bigint NOT NULL CHECK (workload_generation > 0),
  credential_generation bigint NOT NULL CHECK (credential_generation > 0),
  credential_status text NOT NULL CHECK (credential_status IN (
    'CURRENT',
    'NEXT',
    'PREVIOUS',
    'RETIRED'
  )),
  overlap_not_after timestamptz,
  retired_at timestamptz,
  CHECK (
    (credential_status = 'PREVIOUS' AND overlap_not_after IS NOT NULL AND retired_at IS NULL)
    OR (credential_status IN ('CURRENT', 'NEXT') AND overlap_not_after IS NULL AND retired_at IS NULL)
    OR (credential_status = 'RETIRED' AND retired_at IS NOT NULL)
  ),
  UNIQUE (workload_id, role, workload_generation, credential_generation)
);
ALTER TABLE internal_rpc_authority.authority_workload_database_identities
  OWNER TO internal_rpc_authority_readback_owner;
CREATE UNIQUE INDEX authority_identity_one_current
  ON internal_rpc_authority.authority_workload_database_identities
    (workload_id, role, workload_generation)
  WHERE credential_status = 'CURRENT';
CREATE UNIQUE INDEX authority_identity_one_next
  ON internal_rpc_authority.authority_workload_database_identities
    (workload_id, role, workload_generation)
  WHERE credential_status = 'NEXT';
CREATE UNIQUE INDEX authority_identity_one_previous
  ON internal_rpc_authority.authority_workload_database_identities
    (workload_id, role, workload_generation)
  WHERE credential_status = 'PREVIOUS';

CREATE TABLE internal_rpc_authority.authority_runtime_database_identities (
  session_login name PRIMARY KEY,
  capability_role text NOT NULL CHECK (capability_role IN (
    'PUBLISHER',
    'READBACK_ATTESTOR'
  )),
  credential_generation bigint NOT NULL CHECK (credential_generation > 0),
  credential_status text NOT NULL CHECK (credential_status IN (
    'CURRENT',
    'NEXT',
    'PREVIOUS',
    'RETIRED'
  )),
  overlap_not_after timestamptz,
  retired_at timestamptz,
  CHECK (
    (credential_status = 'PREVIOUS' AND overlap_not_after IS NOT NULL
      AND retired_at IS NULL)
    OR (credential_status IN ('CURRENT', 'NEXT')
      AND overlap_not_after IS NULL AND retired_at IS NULL)
    OR (credential_status = 'RETIRED' AND retired_at IS NOT NULL)
  ),
  UNIQUE (capability_role, credential_generation)
);
ALTER TABLE internal_rpc_authority.authority_runtime_database_identities
  OWNER TO internal_rpc_authority_readback_owner;
CREATE UNIQUE INDEX authority_runtime_identity_one_current
  ON internal_rpc_authority.authority_runtime_database_identities
    (capability_role)
  WHERE credential_status = 'CURRENT';
CREATE UNIQUE INDEX authority_runtime_identity_one_next
  ON internal_rpc_authority.authority_runtime_database_identities
    (capability_role)
  WHERE credential_status = 'NEXT';
CREATE UNIQUE INDEX authority_runtime_identity_one_previous
  ON internal_rpc_authority.authority_runtime_database_identities
    (capability_role)
  WHERE credential_status = 'PREVIOUS';

CREATE TABLE internal_rpc_authority.authority_readback_intents (
  intent_id uuid PRIMARY KEY,
  intent_kind text NOT NULL CHECK (intent_kind IN ('KEY_DELIVERY', 'SNAPSHOT')),
  intent_revision bigint NOT NULL CHECK (intent_revision > 0),
  workload_id text NOT NULL,
  workload_spiffe_id text NOT NULL
    CHECK (workload_spiffe_id ~ '^spiffe://mattercodex[.]local/'),
  role text NOT NULL CHECK (role IN (
    'AUTHORIZATION_ISSUER',
    'AUTHORIZATION_VERIFIER',
    'AUTHORITY_PROOF_RESOLVER'
  )),
  workload_generation bigint NOT NULL CHECK (workload_generation > 0),
  credential_generation bigint NOT NULL CHECK (credential_generation > 0),
  material_generation bigint NOT NULL CHECK (material_generation > 0),
  readback_purpose text NOT NULL CHECK (readback_purpose IN (
    'KEY_DELIVERY_READBACK',
    'SNAPSHOT_READBACK'
  )),
  readback_credential_jti uuid NOT NULL UNIQUE,
  readback_credential_digest_sha256 text NOT NULL UNIQUE
    CHECK (readback_credential_digest_sha256 ~ '^[a-f0-9]{64}$'),
  possession_key_kid text NOT NULL
    CHECK (possession_key_kid ~ '^[A-Za-z0-9._-]{3,64}$'),
  possession_key_generation bigint NOT NULL CHECK (possession_key_generation > 0),
  possession_key_thumbprint_sha256 text NOT NULL
    CHECK (possession_key_thumbprint_sha256 ~ '^[a-f0-9]{64}$'),
  readback_credential_signer_generation bigint NOT NULL
    CHECK (readback_credential_signer_generation > 0),
  source_revision bigint NOT NULL CHECK (source_revision > 0),
  digest_sha256 text NOT NULL CHECK (digest_sha256 ~ '^[a-f0-9]{64}$'),
  key_generation bigint CHECK (key_generation > 0),
  key_set_revision bigint CHECK (key_set_revision > 0),
  policy_revision bigint CHECK (policy_revision > 0),
  signer_generation bigint NOT NULL CHECK (signer_generation > 0),
  intent_status text NOT NULL CHECK (intent_status IN ('PINNED', 'PROMOTED', 'RETIRED')),
  pinned_at timestamptz NOT NULL,
  CHECK (
    (intent_kind = 'KEY_DELIVERY' AND key_generation IS NOT NULL
      AND key_set_revision IS NULL AND policy_revision IS NULL
      AND readback_purpose = 'KEY_DELIVERY_READBACK')
    OR
    (intent_kind = 'SNAPSHOT' AND key_generation IS NULL
      AND key_set_revision IS NOT NULL AND policy_revision IS NOT NULL
      AND readback_purpose = 'SNAPSHOT_READBACK')
  ),
  UNIQUE (
    intent_kind,
    workload_id,
    role,
    workload_generation,
    credential_generation,
    source_revision
  ),
  FOREIGN KEY (workload_id, role, workload_generation, credential_generation)
    REFERENCES internal_rpc_authority.authority_workload_database_identities
      (workload_id, role, workload_generation, credential_generation)
);
ALTER TABLE internal_rpc_authority.authority_readback_intents
  OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_readback_intents
  ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_readback_intents
  FORCE ROW LEVEL SECURITY;

CREATE TABLE internal_rpc_authority.authority_readback_attestation_challenges (
  challenge_id uuid PRIMARY KEY,
  challenge_jti uuid NOT NULL UNIQUE,
  challenge_nonce text NOT NULL
    CHECK (challenge_nonce ~ '^[A-Za-z0-9_-]{22,86}$'),
  challenge_digest_sha256 text NOT NULL UNIQUE
    CHECK (challenge_digest_sha256 ~ '^[a-f0-9]{64}$'),
  intent_id uuid NOT NULL REFERENCES
    internal_rpc_authority.authority_readback_intents(intent_id),
  readback_credential_jti uuid NOT NULL,
  readback_credential_digest_sha256 text NOT NULL
    CHECK (readback_credential_digest_sha256 ~ '^[a-f0-9]{64}$'),
  possession_key_generation bigint NOT NULL CHECK (possession_key_generation > 0),
  possession_key_thumbprint_sha256 text NOT NULL
    CHECK (possession_key_thumbprint_sha256 ~ '^[a-f0-9]{64}$'),
  audience text NOT NULL CHECK (
    audience = 'urn:mattercodex:internal-rpc-authority-readback-attestor'
  ),
  idempotency_key uuid NOT NULL UNIQUE,
  semantic_request_digest_sha256 text NOT NULL
    CHECK (semantic_request_digest_sha256 ~ '^[a-f0-9]{64}$'),
  challenge_status text NOT NULL CHECK (challenge_status IN (
    'ISSUED',
    'CONSUMED',
    'EXPIRED'
  )),
  issued_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  CHECK (
    expires_at = issued_at + interval '30 seconds'
    AND (
      (challenge_status = 'ISSUED' AND consumed_at IS NULL)
      OR (challenge_status = 'CONSUMED' AND consumed_at IS NOT NULL)
      OR (challenge_status = 'EXPIRED' AND consumed_at IS NULL)
    )
  ),
  UNIQUE (
    intent_id,
    readback_credential_jti,
    challenge_id
  )
);
ALTER TABLE internal_rpc_authority.authority_readback_attestation_challenges
  OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_readback_attestation_challenges
  ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_readback_attestation_challenges
  FORCE ROW LEVEL SECURITY;

CREATE TABLE internal_rpc_authority.authority_readback_attestation_receipts (
  receipt_id uuid PRIMARY KEY,
  challenge_id uuid NOT NULL UNIQUE REFERENCES
    internal_rpc_authority.authority_readback_attestation_challenges(challenge_id),
  intent_id uuid NOT NULL REFERENCES
    internal_rpc_authority.authority_readback_intents(intent_id),
  session_login name NOT NULL,
  workload_id text NOT NULL,
  role text NOT NULL,
  workload_generation bigint NOT NULL CHECK (workload_generation > 0),
  credential_generation bigint NOT NULL CHECK (credential_generation > 0),
  readback_credential_jti uuid NOT NULL,
  readback_credential_digest_sha256 text NOT NULL
    CHECK (readback_credential_digest_sha256 ~ '^[a-f0-9]{64}$'),
  evidence_jti uuid NOT NULL UNIQUE,
  evidence_digest_sha256 text NOT NULL UNIQUE
    CHECK (evidence_digest_sha256 ~ '^[a-f0-9]{64}$'),
  served_state_digest_sha256 text NOT NULL
    CHECK (served_state_digest_sha256 ~ '^[a-f0-9]{64}$'),
  public_key_thumbprint_sha256 text NOT NULL
    CHECK (public_key_thumbprint_sha256 ~ '^[a-f0-9]{64}$'),
  verifier_generation bigint NOT NULL CHECK (verifier_generation > 0),
  idempotency_key uuid NOT NULL UNIQUE,
  semantic_request_digest_sha256 text NOT NULL
    CHECK (semantic_request_digest_sha256 ~ '^[a-f0-9]{64}$'),
  verification_method text NOT NULL
    CHECK (
      verification_method =
        'ES256_NORMAL_READBACK_POSSESSION_CHALLENGE_V1'
    ),
  verified_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  CHECK (expires_at > verified_at),
  FOREIGN KEY (workload_id, role, workload_generation, credential_generation)
    REFERENCES internal_rpc_authority.authority_workload_database_identities
      (workload_id, role, workload_generation, credential_generation),
  UNIQUE (receipt_id, intent_id),
  UNIQUE (receipt_id, intent_id, session_login)
);
ALTER TABLE internal_rpc_authority.authority_readback_attestation_receipts
  OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_readback_attestation_receipts
  ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_readback_attestation_receipts
  FORCE ROW LEVEL SECURITY;

CREATE TABLE internal_rpc_authority.authority_key_delivery_readbacks (
  readback_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  receipt_id uuid NOT NULL UNIQUE,
  intent_id uuid NOT NULL,
  workload_id text NOT NULL,
  role text NOT NULL,
  workload_generation bigint NOT NULL,
  credential_generation bigint NOT NULL,
  source_revision bigint NOT NULL CHECK (source_revision > 0),
  digest_sha256 text NOT NULL CHECK (digest_sha256 ~ '^[a-f0-9]{64}$'),
  key_generation bigint NOT NULL CHECK (key_generation > 0),
  signer_generation bigint NOT NULL CHECK (signer_generation > 0),
  evidence_digest_sha256 text NOT NULL CHECK (evidence_digest_sha256 ~ '^[a-f0-9]{64}$'),
  recorded_at timestamptz NOT NULL DEFAULT pg_catalog.clock_timestamp(),
  UNIQUE (
    workload_id,
    role,
    workload_generation,
    credential_generation,
    source_revision
  ),
  FOREIGN KEY (workload_id, role, workload_generation, credential_generation)
    REFERENCES internal_rpc_authority.authority_workload_database_identities
      (workload_id, role, workload_generation, credential_generation),
  FOREIGN KEY (receipt_id, intent_id)
    REFERENCES internal_rpc_authority.authority_readback_attestation_receipts
      (receipt_id, intent_id)
);
ALTER TABLE internal_rpc_authority.authority_key_delivery_readbacks
  OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_key_delivery_readbacks
  ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_key_delivery_readbacks
  FORCE ROW LEVEL SECURITY;

CREATE TABLE internal_rpc_authority.authority_snapshot_readbacks (
  readback_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  receipt_id uuid NOT NULL UNIQUE,
  intent_id uuid NOT NULL,
  workload_id text NOT NULL,
  role text NOT NULL,
  workload_generation bigint NOT NULL,
  credential_generation bigint NOT NULL,
  source_revision bigint NOT NULL CHECK (source_revision > 0),
  digest_sha256 text NOT NULL CHECK (digest_sha256 ~ '^[a-f0-9]{64}$'),
  key_set_revision bigint NOT NULL CHECK (key_set_revision > 0),
  policy_revision bigint NOT NULL CHECK (policy_revision > 0),
  signer_generation bigint NOT NULL CHECK (signer_generation > 0),
  evidence_digest_sha256 text NOT NULL CHECK (evidence_digest_sha256 ~ '^[a-f0-9]{64}$'),
  recorded_at timestamptz NOT NULL DEFAULT pg_catalog.clock_timestamp(),
  UNIQUE (
    workload_id,
    role,
    workload_generation,
    credential_generation,
    source_revision
  ),
  FOREIGN KEY (workload_id, role, workload_generation, credential_generation)
    REFERENCES internal_rpc_authority.authority_workload_database_identities
      (workload_id, role, workload_generation, credential_generation),
  FOREIGN KEY (receipt_id, intent_id)
    REFERENCES internal_rpc_authority.authority_readback_attestation_receipts
      (receipt_id, intent_id)
);
ALTER TABLE internal_rpc_authority.authority_snapshot_readbacks
  OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_snapshot_readbacks
  ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_snapshot_readbacks
  FORCE ROW LEVEL SECURITY;

CREATE FUNCTION internal_rpc_authority.is_active_runtime_database_session(
  p_capability_role text
) RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
  identity internal_rpc_authority.authority_runtime_database_identities%ROWTYPE;
BEGIN
  SELECT * INTO identity
    FROM internal_rpc_authority.authority_runtime_database_identities
   WHERE session_login = session_user
     AND capability_role = p_capability_role
   FOR SHARE;
  IF NOT FOUND THEN
    RETURN false;
  END IF;
  RETURN identity.credential_status IN ('CURRENT', 'NEXT')
    OR (
      identity.credential_status = 'PREVIOUS'
      AND identity.overlap_not_after >= pg_catalog.clock_timestamp()
    );
END
$function$;

CREATE POLICY readback_owner_intents
  ON internal_rpc_authority.authority_readback_intents
  FOR ALL TO internal_rpc_authority_readback_owner
  USING (true) WITH CHECK (true);
CREATE POLICY readback_owner_receipts
  ON internal_rpc_authority.authority_readback_attestation_receipts
  FOR ALL TO internal_rpc_authority_readback_owner
  USING (true) WITH CHECK (true);
CREATE POLICY readback_owner_challenges
  ON internal_rpc_authority.authority_readback_attestation_challenges
  FOR ALL TO internal_rpc_authority_readback_owner
  USING (true) WITH CHECK (true);
CREATE POLICY readback_owner_key_delivery
  ON internal_rpc_authority.authority_key_delivery_readbacks
  FOR ALL TO internal_rpc_authority_readback_owner
  USING (true) WITH CHECK (true);
CREATE POLICY readback_owner_snapshot
  ON internal_rpc_authority.authority_snapshot_readbacks
  FOR ALL TO internal_rpc_authority_readback_owner
  USING (true) WITH CHECK (true);
CREATE POLICY readback_attestor_insert_intent
  ON internal_rpc_authority.authority_readback_intents
  FOR INSERT TO internal_rpc_authority_readback_attestor
  WITH CHECK (
    intent_status = 'PINNED'
    AND internal_rpc_authority.is_active_runtime_database_session(
      'READBACK_ATTESTOR'
    )
  );
CREATE POLICY readback_attestor_select_intent
  ON internal_rpc_authority.authority_readback_intents
  FOR SELECT TO internal_rpc_authority_readback_attestor
  USING (
    internal_rpc_authority.is_active_runtime_database_session(
      'READBACK_ATTESTOR'
    )
  );
CREATE POLICY readback_attestor_select_challenge
  ON internal_rpc_authority.authority_readback_attestation_challenges
  FOR SELECT TO internal_rpc_authority_readback_attestor
  USING (
    internal_rpc_authority.is_active_runtime_database_session(
      'READBACK_ATTESTOR'
    )
  );
CREATE POLICY readback_attestor_select_receipt
  ON internal_rpc_authority.authority_readback_attestation_receipts
  FOR SELECT TO internal_rpc_authority_readback_attestor
  USING (
    internal_rpc_authority.is_active_runtime_database_session(
      'READBACK_ATTESTOR'
    )
  );
CREATE POLICY key_delivery_publisher_read
  ON internal_rpc_authority.authority_key_delivery_readbacks
  FOR SELECT TO internal_rpc_authority_publisher
  USING (
    internal_rpc_authority.is_active_runtime_database_session('PUBLISHER')
  );
CREATE POLICY snapshot_publisher_read
  ON internal_rpc_authority.authority_snapshot_readbacks
  FOR SELECT TO internal_rpc_authority_publisher
  USING (
    internal_rpc_authority.is_active_runtime_database_session('PUBLISHER')
  );

-- Параметры challenge_id/JTI/nonce/digest создаёт серверный
-- AuthorityReadbackAttestorService после проверки mTLS и credential; клиентский
-- Proto request этих полей не содержит. Функция атомарно резервирует результат
-- для multi-replica server process и не выдаёт runtime principal прямой INSERT.
CREATE FUNCTION internal_rpc_authority.issue_authority_readback_attestation_challenge(
  p_intent_id uuid,
  p_challenge_id uuid,
  p_challenge_jti uuid,
  p_challenge_nonce text,
  p_challenge_digest_sha256 text,
  p_readback_credential_jti uuid,
  p_readback_credential_digest_sha256 text,
  p_idempotency_key uuid,
  p_semantic_request_digest_sha256 text
) RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
  intent internal_rpc_authority.authority_readback_intents%ROWTYPE;
  saved internal_rpc_authority.authority_readback_attestation_challenges%ROWTYPE;
  issued_at timestamptz;
BEGIN
  IF NOT internal_rpc_authority.is_active_runtime_database_session(
    'READBACK_ATTESTOR'
  ) THEN
    RAISE EXCEPTION 'readback attestor runtime database identity rejected';
  END IF;

  SELECT * INTO saved
    FROM internal_rpc_authority.authority_readback_attestation_challenges
   WHERE idempotency_key = p_idempotency_key
   FOR UPDATE;
  IF FOUND THEN
    IF saved.intent_id = p_intent_id
       AND saved.readback_credential_jti = p_readback_credential_jti
       AND saved.readback_credential_digest_sha256 =
         p_readback_credential_digest_sha256
       AND saved.semantic_request_digest_sha256 =
         p_semantic_request_digest_sha256 THEN
      RETURN saved.challenge_id;
    END IF;
    RAISE EXCEPTION 'readback challenge idempotency conflict';
  END IF;

  SELECT * INTO STRICT intent
    FROM internal_rpc_authority.authority_readback_intents
   WHERE intent_id = p_intent_id
     AND intent_status = 'PINNED'
     AND readback_credential_jti = p_readback_credential_jti
     AND readback_credential_digest_sha256 =
       p_readback_credential_digest_sha256
   FOR UPDATE;
  issued_at := pg_catalog.clock_timestamp();

  BEGIN
    INSERT INTO internal_rpc_authority.authority_readback_attestation_challenges (
      challenge_id,
      challenge_jti,
      challenge_nonce,
      challenge_digest_sha256,
      intent_id,
      readback_credential_jti,
      readback_credential_digest_sha256,
      possession_key_generation,
      possession_key_thumbprint_sha256,
      audience,
      idempotency_key,
      semantic_request_digest_sha256,
      challenge_status,
      issued_at,
      expires_at
    ) VALUES (
      p_challenge_id,
      p_challenge_jti,
      p_challenge_nonce,
      p_challenge_digest_sha256,
      intent.intent_id,
      intent.readback_credential_jti,
      intent.readback_credential_digest_sha256,
      intent.possession_key_generation,
      intent.possession_key_thumbprint_sha256,
      'urn:mattercodex:internal-rpc-authority-readback-attestor',
      p_idempotency_key,
      p_semantic_request_digest_sha256,
      'ISSUED',
      issued_at,
      issued_at + interval '30 seconds'
    );
  EXCEPTION
    WHEN unique_violation THEN
      SELECT * INTO saved
        FROM internal_rpc_authority.authority_readback_attestation_challenges
       WHERE idempotency_key = p_idempotency_key
       FOR UPDATE;
      IF FOUND
         AND saved.intent_id = p_intent_id
         AND saved.readback_credential_jti = p_readback_credential_jti
         AND saved.readback_credential_digest_sha256 =
           p_readback_credential_digest_sha256
         AND saved.semantic_request_digest_sha256 =
           p_semantic_request_digest_sha256 THEN
        RETURN saved.challenge_id;
      END IF;
      RAISE;
  END;
  RETURN p_challenge_id;
EXCEPTION
  WHEN NO_DATA_FOUND THEN
    RAISE EXCEPTION 'readback challenge pinned intent rejected';
END
$function$;

CREATE FUNCTION internal_rpc_authority.enforce_readback_challenge_consumption()
RETURNS trigger
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
  challenge internal_rpc_authority.authority_readback_attestation_challenges%ROWTYPE;
  intent internal_rpc_authority.authority_readback_intents%ROWTYPE;
  identity internal_rpc_authority.authority_workload_database_identities%ROWTYPE;
BEGIN
  SELECT * INTO STRICT challenge
    FROM internal_rpc_authority.authority_readback_attestation_challenges
   WHERE challenge_id = NEW.challenge_id
   FOR UPDATE;
  IF challenge.challenge_status <> 'ISSUED'
     OR challenge.expires_at <= pg_catalog.clock_timestamp() THEN
    RAISE EXCEPTION 'readback attestation challenge rejected';
  END IF;

  SELECT * INTO STRICT intent
    FROM internal_rpc_authority.authority_readback_intents
   WHERE intent_id = challenge.intent_id
     AND intent_status = 'PINNED'
     AND readback_credential_jti = challenge.readback_credential_jti
     AND readback_credential_digest_sha256 =
       challenge.readback_credential_digest_sha256
     AND possession_key_generation = challenge.possession_key_generation
     AND possession_key_thumbprint_sha256 =
       challenge.possession_key_thumbprint_sha256
   FOR UPDATE;
  SELECT * INTO STRICT identity
    FROM internal_rpc_authority.authority_workload_database_identities
   WHERE workload_id = intent.workload_id
     AND workload_spiffe_id = intent.workload_spiffe_id
     AND role = intent.role
     AND workload_generation = intent.workload_generation
     AND credential_generation = intent.credential_generation
     AND (
       credential_status IN ('CURRENT', 'NEXT')
       OR (
         credential_status = 'PREVIOUS'
         AND overlap_not_after >= pg_catalog.clock_timestamp()
       )
     )
   FOR UPDATE;

  IF NEW.intent_id <> intent.intent_id
     OR NEW.served_state_digest_sha256 <> intent.digest_sha256
     OR NEW.readback_credential_jti <> challenge.readback_credential_jti
     OR NEW.readback_credential_digest_sha256 <>
       challenge.readback_credential_digest_sha256
     OR NEW.public_key_thumbprint_sha256 <>
       challenge.possession_key_thumbprint_sha256
     OR NEW.verification_method <>
       'ES256_NORMAL_READBACK_POSSESSION_CHALLENGE_V1'
     OR NEW.expires_at <> NEW.verified_at + interval '5 minutes' THEN
    RAISE EXCEPTION 'readback attestation receipt binding rejected';
  END IF;

  NEW.session_login := identity.session_login;
  NEW.workload_id := intent.workload_id;
  NEW.role := intent.role;
  NEW.workload_generation := intent.workload_generation;
  NEW.credential_generation := intent.credential_generation;

  UPDATE internal_rpc_authority.authority_readback_attestation_challenges
     SET challenge_status = 'CONSUMED',
         consumed_at = pg_catalog.clock_timestamp()
   WHERE challenge_id = challenge.challenge_id
     AND challenge_status = 'ISSUED';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'readback attestation challenge replay detected';
  END IF;
  RETURN NEW;
EXCEPTION
  WHEN NO_DATA_FOUND THEN
    RAISE EXCEPTION 'readback attestation challenge binding rejected';
END
$function$;

CREATE TRIGGER authority_readback_challenge_consume
BEFORE INSERT ON internal_rpc_authority.authority_readback_attestation_receipts
FOR EACH ROW
EXECUTE FUNCTION internal_rpc_authority.enforce_readback_challenge_consumption();

CREATE FUNCTION internal_rpc_authority.consume_authority_readback_attestation_challenge(
  p_challenge_id uuid,
  p_receipt_id uuid,
  p_evidence_jti uuid,
  p_evidence_digest_sha256 text,
  p_verifier_generation bigint,
  p_idempotency_key uuid,
  p_semantic_request_digest_sha256 text
) RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
  challenge internal_rpc_authority.authority_readback_attestation_challenges%ROWTYPE;
  intent internal_rpc_authority.authority_readback_intents%ROWTYPE;
  saved internal_rpc_authority.authority_readback_attestation_receipts%ROWTYPE;
  verified_at timestamptz;
BEGIN
  IF NOT internal_rpc_authority.is_active_runtime_database_session(
    'READBACK_ATTESTOR'
  ) THEN
    RAISE EXCEPTION 'readback attestor runtime database identity rejected';
  END IF;

  SELECT * INTO saved
    FROM internal_rpc_authority.authority_readback_attestation_receipts
   WHERE idempotency_key = p_idempotency_key
   FOR UPDATE;
  IF FOUND THEN
    IF saved.challenge_id = p_challenge_id
       AND saved.evidence_jti = p_evidence_jti
       AND saved.evidence_digest_sha256 = p_evidence_digest_sha256
       AND saved.semantic_request_digest_sha256 =
         p_semantic_request_digest_sha256 THEN
      RETURN saved.receipt_id;
    END IF;
    RAISE EXCEPTION 'readback attestation idempotency replay detected';
  END IF;

  SELECT * INTO STRICT challenge
    FROM internal_rpc_authority.authority_readback_attestation_challenges
   WHERE challenge_id = p_challenge_id;
  SELECT * INTO STRICT intent
    FROM internal_rpc_authority.authority_readback_intents
   WHERE intent_id = challenge.intent_id;
  verified_at := pg_catalog.clock_timestamp();

  BEGIN
    INSERT INTO internal_rpc_authority.authority_readback_attestation_receipts (
      receipt_id,
      challenge_id,
      intent_id,
      session_login,
      workload_id,
      role,
      workload_generation,
      credential_generation,
      readback_credential_jti,
      readback_credential_digest_sha256,
      evidence_jti,
      evidence_digest_sha256,
      served_state_digest_sha256,
      public_key_thumbprint_sha256,
      verifier_generation,
      idempotency_key,
      semantic_request_digest_sha256,
      verification_method,
      verified_at,
      expires_at
    ) VALUES (
      p_receipt_id,
      challenge.challenge_id,
      intent.intent_id,
      NULL,
      NULL,
      NULL,
      NULL,
      NULL,
      challenge.readback_credential_jti,
      challenge.readback_credential_digest_sha256,
      p_evidence_jti,
      p_evidence_digest_sha256,
      intent.digest_sha256,
      challenge.possession_key_thumbprint_sha256,
      p_verifier_generation,
      p_idempotency_key,
      p_semantic_request_digest_sha256,
      'ES256_NORMAL_READBACK_POSSESSION_CHALLENGE_V1',
      verified_at,
      verified_at + interval '5 minutes'
    );
  EXCEPTION
    WHEN unique_violation OR raise_exception THEN
      SELECT * INTO saved
        FROM internal_rpc_authority.authority_readback_attestation_receipts
       WHERE idempotency_key = p_idempotency_key
       FOR UPDATE;
      IF FOUND
         AND saved.challenge_id = p_challenge_id
         AND saved.evidence_jti = p_evidence_jti
         AND saved.evidence_digest_sha256 = p_evidence_digest_sha256
         AND saved.semantic_request_digest_sha256 =
           p_semantic_request_digest_sha256 THEN
        RETURN saved.receipt_id;
      END IF;
      RAISE;
  END;
  RETURN p_receipt_id;
EXCEPTION
  WHEN NO_DATA_FOUND THEN
    RAISE EXCEPTION 'readback attestation challenge binding rejected';
END
$function$;

CREATE FUNCTION internal_rpc_authority.record_authority_key_delivery_readback(
  p_attestation_receipt_id uuid
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
  identity internal_rpc_authority.authority_workload_database_identities%ROWTYPE;
  receipt internal_rpc_authority.authority_readback_attestation_receipts%ROWTYPE;
  intent internal_rpc_authority.authority_readback_intents%ROWTYPE;
  result_id bigint;
BEGIN
  SELECT * INTO STRICT identity
    FROM internal_rpc_authority.authority_workload_database_identities
   WHERE session_login = session_user
     AND (
       credential_status IN ('CURRENT', 'NEXT')
       OR (
         credential_status = 'PREVIOUS'
         AND overlap_not_after >= pg_catalog.clock_timestamp()
       )
     )
   FOR UPDATE;

  SELECT attestation.*
    INTO STRICT receipt
    FROM internal_rpc_authority.authority_readback_attestation_receipts AS attestation
    JOIN internal_rpc_authority.authority_readback_intents AS pinned
      ON pinned.intent_id = attestation.intent_id
   WHERE attestation.receipt_id = p_attestation_receipt_id
     AND pinned.intent_kind = 'KEY_DELIVERY'
     AND pinned.intent_status = 'PINNED'
     AND attestation.session_login = session_user
     AND attestation.workload_id = identity.workload_id
     AND attestation.role = identity.role
     AND attestation.workload_generation = identity.workload_generation
     AND attestation.credential_generation = identity.credential_generation
     AND pinned.workload_id = identity.workload_id
     AND pinned.workload_spiffe_id = identity.workload_spiffe_id
     AND pinned.role = identity.role
     AND pinned.workload_generation = identity.workload_generation
     AND pinned.credential_generation = identity.credential_generation
     AND attestation.served_state_digest_sha256 = pinned.digest_sha256
     AND attestation.verification_method =
       'ES256_NORMAL_READBACK_POSSESSION_CHALLENGE_V1'
     AND attestation.consumed_at IS NULL
     AND attestation.expires_at > pg_catalog.clock_timestamp()
   FOR UPDATE OF attestation, pinned;
  SELECT * INTO STRICT intent
    FROM internal_rpc_authority.authority_readback_intents
   WHERE intent_id = receipt.intent_id;

  INSERT INTO internal_rpc_authority.authority_key_delivery_readbacks (
    receipt_id, intent_id, workload_id, role, workload_generation,
    credential_generation, source_revision, digest_sha256, key_generation,
    signer_generation, evidence_digest_sha256
  ) VALUES (
    receipt.receipt_id, intent.intent_id, identity.workload_id, identity.role,
    identity.workload_generation, identity.credential_generation,
    intent.source_revision, intent.digest_sha256, intent.key_generation,
    intent.signer_generation, receipt.evidence_digest_sha256
  ) RETURNING readback_id INTO result_id;

  UPDATE internal_rpc_authority.authority_readback_attestation_receipts
     SET consumed_at = pg_catalog.clock_timestamp()
   WHERE receipt_id = receipt.receipt_id
     AND consumed_at IS NULL;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'readback attestation receipt already consumed';
  END IF;
  RETURN result_id;
EXCEPTION
  WHEN NO_DATA_FOUND THEN
    RAISE EXCEPTION 'key delivery readback attestation rejected';
END
$function$;

CREATE FUNCTION internal_rpc_authority.record_authority_snapshot_readback(
  p_attestation_receipt_id uuid
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
  identity internal_rpc_authority.authority_workload_database_identities%ROWTYPE;
  receipt internal_rpc_authority.authority_readback_attestation_receipts%ROWTYPE;
  intent internal_rpc_authority.authority_readback_intents%ROWTYPE;
  result_id bigint;
BEGIN
  SELECT * INTO STRICT identity
    FROM internal_rpc_authority.authority_workload_database_identities
   WHERE session_login = session_user
     AND (
       credential_status IN ('CURRENT', 'NEXT')
       OR (
         credential_status = 'PREVIOUS'
         AND overlap_not_after >= pg_catalog.clock_timestamp()
       )
     )
   FOR UPDATE;

  SELECT attestation.*
    INTO STRICT receipt
    FROM internal_rpc_authority.authority_readback_attestation_receipts AS attestation
    JOIN internal_rpc_authority.authority_readback_intents AS pinned
      ON pinned.intent_id = attestation.intent_id
   WHERE attestation.receipt_id = p_attestation_receipt_id
     AND pinned.intent_kind = 'SNAPSHOT'
     AND pinned.intent_status = 'PINNED'
     AND attestation.session_login = session_user
     AND attestation.workload_id = identity.workload_id
     AND attestation.role = identity.role
     AND attestation.workload_generation = identity.workload_generation
     AND attestation.credential_generation = identity.credential_generation
     AND pinned.workload_id = identity.workload_id
     AND pinned.workload_spiffe_id = identity.workload_spiffe_id
     AND pinned.role = identity.role
     AND pinned.workload_generation = identity.workload_generation
     AND pinned.credential_generation = identity.credential_generation
     AND attestation.served_state_digest_sha256 = pinned.digest_sha256
     AND attestation.verification_method =
       'ES256_NORMAL_READBACK_POSSESSION_CHALLENGE_V1'
     AND attestation.consumed_at IS NULL
     AND attestation.expires_at > pg_catalog.clock_timestamp()
   FOR UPDATE OF attestation, pinned;
  SELECT * INTO STRICT intent
    FROM internal_rpc_authority.authority_readback_intents
   WHERE intent_id = receipt.intent_id;

  INSERT INTO internal_rpc_authority.authority_snapshot_readbacks (
    receipt_id, intent_id, workload_id, role, workload_generation,
    credential_generation, source_revision, digest_sha256, key_set_revision,
    policy_revision, signer_generation, evidence_digest_sha256
  ) VALUES (
    receipt.receipt_id, intent.intent_id, identity.workload_id, identity.role,
    identity.workload_generation, identity.credential_generation,
    intent.source_revision, intent.digest_sha256, intent.key_set_revision,
    intent.policy_revision, intent.signer_generation,
    receipt.evidence_digest_sha256
  ) RETURNING readback_id INTO result_id;

  UPDATE internal_rpc_authority.authority_readback_attestation_receipts
     SET consumed_at = pg_catalog.clock_timestamp()
   WHERE receipt_id = receipt.receipt_id
     AND consumed_at IS NULL;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'readback attestation receipt already consumed';
  END IF;
  RETURN result_id;
EXCEPTION
  WHEN NO_DATA_FOUND THEN
    RAISE EXCEPTION 'snapshot readback attestation rejected';
END
$function$;

CREATE FUNCTION internal_rpc_authority.promote_authority_workload_database_identity(
  p_workload_id text,
  p_role text,
  p_workload_generation bigint,
  p_next_credential_generation bigint,
  p_key_delivery_intent_id uuid,
  p_snapshot_intent_id uuid
) RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
  current_identity internal_rpc_authority.authority_workload_database_identities%ROWTYPE;
  next_identity internal_rpc_authority.authority_workload_database_identities%ROWTYPE;
BEGIN
  IF pg_catalog.current_setting('transaction_isolation') <> 'serializable' THEN
    RAISE EXCEPTION 'credential promotion requires serializable transaction';
  END IF;
  IF NOT internal_rpc_authority.is_active_runtime_database_session(
    'PUBLISHER'
  ) THEN
    RAISE EXCEPTION 'publisher runtime database identity rejected';
  END IF;

  UPDATE internal_rpc_authority.authority_workload_database_identities
     SET credential_status = 'RETIRED',
         retired_at = pg_catalog.clock_timestamp()
   WHERE workload_id = p_workload_id
     AND role = p_role
     AND workload_generation = p_workload_generation
     AND credential_status = 'PREVIOUS'
     AND overlap_not_after < pg_catalog.clock_timestamp();
  IF EXISTS (
    SELECT 1
      FROM internal_rpc_authority.authority_workload_database_identities
     WHERE workload_id = p_workload_id
       AND role = p_role
       AND workload_generation = p_workload_generation
       AND credential_status = 'PREVIOUS'
  ) THEN
    RAISE EXCEPTION 'previous credential overlap is still active';
  END IF;

  SELECT * INTO STRICT current_identity
    FROM internal_rpc_authority.authority_workload_database_identities
   WHERE workload_id = p_workload_id
     AND role = p_role
     AND workload_generation = p_workload_generation
     AND credential_status = 'CURRENT'
   FOR UPDATE;
  SELECT * INTO STRICT next_identity
    FROM internal_rpc_authority.authority_workload_database_identities
   WHERE workload_id = p_workload_id
     AND role = p_role
     AND workload_generation = p_workload_generation
     AND credential_generation = p_next_credential_generation
     AND credential_status = 'NEXT'
   FOR UPDATE;

  PERFORM 1
    FROM internal_rpc_authority.authority_key_delivery_readbacks AS key_readback
    JOIN internal_rpc_authority.authority_readback_intents AS intent
      ON intent.intent_id = key_readback.intent_id
   WHERE key_readback.intent_id = p_key_delivery_intent_id
     AND key_readback.workload_id = p_workload_id
     AND key_readback.role = p_role
     AND key_readback.workload_generation = p_workload_generation
     AND key_readback.credential_generation = p_next_credential_generation
     AND intent.intent_status = 'PINNED'
   FOR UPDATE OF intent;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'next key delivery readback is incomplete';
  END IF;
  PERFORM 1
    FROM internal_rpc_authority.authority_snapshot_readbacks AS snapshot_readback
    JOIN internal_rpc_authority.authority_readback_intents AS intent
      ON intent.intent_id = snapshot_readback.intent_id
   WHERE snapshot_readback.intent_id = p_snapshot_intent_id
     AND snapshot_readback.workload_id = p_workload_id
     AND snapshot_readback.role = p_role
     AND snapshot_readback.workload_generation = p_workload_generation
     AND snapshot_readback.credential_generation = p_next_credential_generation
     AND intent.intent_status = 'PINNED'
   FOR UPDATE OF intent;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'next snapshot readback is incomplete';
  END IF;

  UPDATE internal_rpc_authority.authority_workload_database_identities
     SET credential_status = 'PREVIOUS',
         overlap_not_after = pg_catalog.clock_timestamp() + interval '40 seconds'
   WHERE session_login = current_identity.session_login;
  UPDATE internal_rpc_authority.authority_workload_database_identities
     SET credential_status = 'CURRENT'
   WHERE session_login = next_identity.session_login;
  UPDATE internal_rpc_authority.authority_readback_intents
     SET intent_status = 'PROMOTED'
   WHERE intent_id IN (p_key_delivery_intent_id, p_snapshot_intent_id)
     AND intent_status = 'PINNED';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'pinned readback intent promotion failed';
  END IF;
END
$function$;

ALTER FUNCTION internal_rpc_authority.record_authority_key_delivery_readback(uuid)
  OWNER TO internal_rpc_authority_readback_owner;
ALTER FUNCTION internal_rpc_authority.is_active_runtime_database_session(text)
  OWNER TO internal_rpc_authority_readback_owner;
ALTER FUNCTION internal_rpc_authority.enforce_readback_challenge_consumption()
  OWNER TO internal_rpc_authority_readback_owner;
ALTER FUNCTION internal_rpc_authority.issue_authority_readback_attestation_challenge(
  uuid, uuid, uuid, text, text, uuid, text, uuid, text
) OWNER TO internal_rpc_authority_readback_owner;
ALTER FUNCTION internal_rpc_authority.consume_authority_readback_attestation_challenge(
  uuid, uuid, uuid, text, bigint, uuid, text
) OWNER TO internal_rpc_authority_readback_owner;
ALTER FUNCTION internal_rpc_authority.record_authority_snapshot_readback(uuid)
  OWNER TO internal_rpc_authority_readback_owner;
ALTER FUNCTION internal_rpc_authority.promote_authority_workload_database_identity(
  text, text, bigint, bigint, uuid, uuid
) OWNER TO internal_rpc_authority_readback_owner;
REVOKE ALL ON FUNCTION
  internal_rpc_authority.is_active_runtime_database_session(text)
  FROM PUBLIC;
REVOKE ALL ON FUNCTION
  internal_rpc_authority.enforce_readback_challenge_consumption()
  FROM PUBLIC;
REVOKE ALL ON FUNCTION
  internal_rpc_authority.issue_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, text, uuid, text, uuid, text
  ) FROM PUBLIC;
REVOKE ALL ON FUNCTION
  internal_rpc_authority.consume_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, bigint, uuid, text
  ) FROM PUBLIC;
REVOKE ALL ON FUNCTION
  internal_rpc_authority.record_authority_key_delivery_readback(uuid)
  FROM PUBLIC;
REVOKE ALL ON FUNCTION
  internal_rpc_authority.record_authority_snapshot_readback(uuid)
  FROM PUBLIC;
REVOKE ALL ON FUNCTION
  internal_rpc_authority.promote_authority_workload_database_identity(
    text, text, bigint, bigint, uuid, uuid
  ) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION
  internal_rpc_authority.is_active_runtime_database_session(text)
  TO internal_rpc_authority_publisher,
    internal_rpc_authority_readback_attestor;
GRANT EXECUTE ON FUNCTION
  internal_rpc_authority.issue_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, text, uuid, text, uuid, text
  ) TO internal_rpc_authority_readback_attestor;
GRANT EXECUTE ON FUNCTION
  internal_rpc_authority.consume_authority_readback_attestation_challenge(
    uuid, uuid, uuid, text, bigint, uuid, text
  ) TO internal_rpc_authority_readback_attestor;
GRANT EXECUTE ON FUNCTION
  internal_rpc_authority.record_authority_key_delivery_readback(uuid)
  TO
    ira_control_api_gateway_issuer_g1,
    ira_control_api_gateway_issuer_g2,
    ira_control_plane_verifier_g1,
    ira_control_plane_verifier_g2,
    ira_control_plane_resolver_g1,
    ira_control_plane_resolver_g2;
GRANT EXECUTE ON FUNCTION
  internal_rpc_authority.record_authority_snapshot_readback(uuid)
  TO
    ira_control_api_gateway_issuer_g1,
    ira_control_api_gateway_issuer_g2,
    ira_control_plane_verifier_g1,
    ira_control_plane_verifier_g2,
    ira_control_plane_resolver_g1,
    ira_control_plane_resolver_g2;
GRANT EXECUTE ON FUNCTION
  internal_rpc_authority.promote_authority_workload_database_identity(
    text, text, bigint, bigint, uuid, uuid
  ) TO internal_rpc_authority_publisher;

REVOKE ALL ON
  internal_rpc_authority.authority_runtime_database_identities
FROM PUBLIC,
  internal_rpc_authority_publisher,
  internal_rpc_authority_readback_attestor,
  ira_publisher_g1,
  ira_publisher_g2,
  ira_readback_attestor_g1,
  ira_readback_attestor_g2;
REVOKE ALL ON
  internal_rpc_authority.authority_readback_intents,
  internal_rpc_authority.authority_readback_attestation_challenges,
  internal_rpc_authority.authority_readback_attestation_receipts,
  internal_rpc_authority.authority_key_delivery_readbacks,
  internal_rpc_authority.authority_snapshot_readbacks
FROM PUBLIC,
  ira_control_api_gateway_issuer_g1,
  ira_control_api_gateway_issuer_g2,
  ira_control_plane_verifier_g1,
  ira_control_plane_verifier_g2,
  ira_control_plane_resolver_g1,
  ira_control_plane_resolver_g2;
GRANT SELECT ON
  internal_rpc_authority.authority_key_delivery_readbacks,
  internal_rpc_authority.authority_snapshot_readbacks
TO internal_rpc_authority_publisher;
GRANT SELECT, INSERT ON
  internal_rpc_authority.authority_readback_intents
TO internal_rpc_authority_readback_attestor;
GRANT SELECT ON
  internal_rpc_authority.authority_readback_attestation_challenges,
  internal_rpc_authority.authority_readback_attestation_receipts
TO internal_rpc_authority_readback_attestor;

COMMIT;
