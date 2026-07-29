BEGIN;

CREATE ROLE internal_rpc_authority_readback_owner
  NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE internal_rpc_authority_publisher
  NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_control_api_gateway_issuer_g1
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_control_plane_verifier_g1
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE ira_control_plane_resolver_g1
  LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

CREATE SCHEMA internal_rpc_authority
  AUTHORIZATION internal_rpc_authority_readback_owner;
REVOKE ALL ON SCHEMA internal_rpc_authority FROM PUBLIC;
GRANT USAGE ON SCHEMA internal_rpc_authority TO
  internal_rpc_authority_publisher,
  ira_control_api_gateway_issuer_g1,
  ira_control_plane_verifier_g1,
  ira_control_plane_resolver_g1;

CREATE TABLE internal_rpc_authority.authority_workload_database_identities (
  session_login name PRIMARY KEY,
  workload_id text NOT NULL,
  role text NOT NULL CHECK (role IN (
    'AUTHORIZATION_ISSUER',
    'AUTHORIZATION_VERIFIER',
    'AUTHORITY_PROOF_RESOLVER'
  )),
  workload_generation bigint NOT NULL CHECK (workload_generation > 0),
  credential_generation bigint NOT NULL CHECK (credential_generation > 0),
  active boolean NOT NULL,
  UNIQUE (workload_id, role, workload_generation, credential_generation),
  UNIQUE (workload_id, role, workload_generation, active)
);
ALTER TABLE internal_rpc_authority.authority_workload_database_identities
  OWNER TO internal_rpc_authority_readback_owner;

CREATE TABLE internal_rpc_authority.authority_key_delivery_readbacks (
  readback_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  workload_id text NOT NULL,
  role text NOT NULL,
  workload_generation bigint NOT NULL,
  credential_generation bigint NOT NULL,
  source_revision bigint NOT NULL CHECK (source_revision > 0),
  digest_sha256 text NOT NULL CHECK (digest_sha256 ~ '^[a-f0-9]{64}$'),
  key_generation bigint NOT NULL CHECK (key_generation > 0),
  signer_generation bigint NOT NULL CHECK (signer_generation > 0),
  served_proof_sha256 text NOT NULL CHECK (served_proof_sha256 ~ '^[a-f0-9]{64}$'),
  recorded_at timestamptz NOT NULL DEFAULT pg_catalog.clock_timestamp(),
  UNIQUE (workload_id, role, workload_generation, source_revision),
  FOREIGN KEY (workload_id, role, workload_generation, credential_generation)
    REFERENCES internal_rpc_authority.authority_workload_database_identities
      (workload_id, role, workload_generation, credential_generation)
);
ALTER TABLE internal_rpc_authority.authority_key_delivery_readbacks
  OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_key_delivery_readbacks
  ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_key_delivery_readbacks
  FORCE ROW LEVEL SECURITY;

CREATE TABLE internal_rpc_authority.authority_snapshot_readbacks (
  readback_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  workload_id text NOT NULL,
  role text NOT NULL,
  workload_generation bigint NOT NULL,
  credential_generation bigint NOT NULL,
  source_revision bigint NOT NULL CHECK (source_revision > 0),
  digest_sha256 text NOT NULL CHECK (digest_sha256 ~ '^[a-f0-9]{64}$'),
  key_set_revision bigint NOT NULL CHECK (key_set_revision > 0),
  policy_revision bigint NOT NULL CHECK (policy_revision > 0),
  signer_generation bigint NOT NULL CHECK (signer_generation > 0),
  served_proof_sha256 text NOT NULL CHECK (served_proof_sha256 ~ '^[a-f0-9]{64}$'),
  recorded_at timestamptz NOT NULL DEFAULT pg_catalog.clock_timestamp(),
  UNIQUE (workload_id, role, workload_generation, source_revision),
  FOREIGN KEY (workload_id, role, workload_generation, credential_generation)
    REFERENCES internal_rpc_authority.authority_workload_database_identities
      (workload_id, role, workload_generation, credential_generation)
);
ALTER TABLE internal_rpc_authority.authority_snapshot_readbacks
  OWNER TO internal_rpc_authority_readback_owner;
ALTER TABLE internal_rpc_authority.authority_snapshot_readbacks
  ENABLE ROW LEVEL SECURITY;
ALTER TABLE internal_rpc_authority.authority_snapshot_readbacks
  FORCE ROW LEVEL SECURITY;

CREATE POLICY key_delivery_owner_write
  ON internal_rpc_authority.authority_key_delivery_readbacks
  FOR ALL TO internal_rpc_authority_readback_owner
  USING (EXISTS (
    SELECT 1
    FROM internal_rpc_authority.authority_workload_database_identities AS identity
    WHERE identity.session_login = session_user
      AND identity.active
      AND identity.workload_id = authority_key_delivery_readbacks.workload_id
      AND identity.role = authority_key_delivery_readbacks.role
      AND identity.workload_generation = authority_key_delivery_readbacks.workload_generation
      AND identity.credential_generation = authority_key_delivery_readbacks.credential_generation
  ))
  WITH CHECK (EXISTS (
    SELECT 1
    FROM internal_rpc_authority.authority_workload_database_identities AS identity
    WHERE identity.session_login = session_user
      AND identity.active
      AND identity.workload_id = authority_key_delivery_readbacks.workload_id
      AND identity.role = authority_key_delivery_readbacks.role
      AND identity.workload_generation = authority_key_delivery_readbacks.workload_generation
      AND identity.credential_generation = authority_key_delivery_readbacks.credential_generation
  ));
CREATE POLICY snapshot_owner_write
  ON internal_rpc_authority.authority_snapshot_readbacks
  FOR ALL TO internal_rpc_authority_readback_owner
  USING (EXISTS (
    SELECT 1
    FROM internal_rpc_authority.authority_workload_database_identities AS identity
    WHERE identity.session_login = session_user
      AND identity.active
      AND identity.workload_id = authority_snapshot_readbacks.workload_id
      AND identity.role = authority_snapshot_readbacks.role
      AND identity.workload_generation = authority_snapshot_readbacks.workload_generation
      AND identity.credential_generation = authority_snapshot_readbacks.credential_generation
  ))
  WITH CHECK (EXISTS (
    SELECT 1
    FROM internal_rpc_authority.authority_workload_database_identities AS identity
    WHERE identity.session_login = session_user
      AND identity.active
      AND identity.workload_id = authority_snapshot_readbacks.workload_id
      AND identity.role = authority_snapshot_readbacks.role
      AND identity.workload_generation = authority_snapshot_readbacks.workload_generation
      AND identity.credential_generation = authority_snapshot_readbacks.credential_generation
  ));
CREATE POLICY key_delivery_publisher_read
  ON internal_rpc_authority.authority_key_delivery_readbacks
  FOR SELECT TO internal_rpc_authority_publisher USING (true);
CREATE POLICY snapshot_publisher_read
  ON internal_rpc_authority.authority_snapshot_readbacks
  FOR SELECT TO internal_rpc_authority_publisher USING (true);

CREATE FUNCTION internal_rpc_authority.record_authority_key_delivery_readback(
  p_source_revision bigint,
  p_digest_sha256 text,
  p_key_generation bigint,
  p_signer_generation bigint,
  p_served_proof_sha256 text
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
  identity internal_rpc_authority.authority_workload_database_identities%ROWTYPE;
  result_id bigint;
BEGIN
  SELECT *
    INTO STRICT identity
    FROM internal_rpc_authority.authority_workload_database_identities
   WHERE session_login = session_user
     AND active
   FOR UPDATE;
  INSERT INTO internal_rpc_authority.authority_key_delivery_readbacks (
    workload_id, role, workload_generation, credential_generation,
    source_revision, digest_sha256, key_generation, signer_generation,
    served_proof_sha256
  ) VALUES (
    identity.workload_id, identity.role, identity.workload_generation,
    identity.credential_generation, p_source_revision, p_digest_sha256,
    p_key_generation, p_signer_generation, p_served_proof_sha256
  )
  ON CONFLICT (workload_id, role, workload_generation, source_revision)
  DO NOTHING
  RETURNING readback_id INTO result_id;
  IF result_id IS NULL THEN
    SELECT readback_id INTO STRICT result_id
      FROM internal_rpc_authority.authority_key_delivery_readbacks
     WHERE workload_id = identity.workload_id
       AND role = identity.role
       AND workload_generation = identity.workload_generation
       AND source_revision = p_source_revision
       AND digest_sha256 = p_digest_sha256
       AND key_generation = p_key_generation
       AND signer_generation = p_signer_generation
       AND served_proof_sha256 = p_served_proof_sha256;
  END IF;
  RETURN result_id;
END
$function$;

CREATE FUNCTION internal_rpc_authority.record_authority_snapshot_readback(
  p_source_revision bigint,
  p_digest_sha256 text,
  p_key_set_revision bigint,
  p_policy_revision bigint,
  p_signer_generation bigint,
  p_served_proof_sha256 text
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, internal_rpc_authority, pg_temp
AS $function$
DECLARE
  identity internal_rpc_authority.authority_workload_database_identities%ROWTYPE;
  result_id bigint;
BEGIN
  SELECT *
    INTO STRICT identity
    FROM internal_rpc_authority.authority_workload_database_identities
   WHERE session_login = session_user
     AND active
   FOR UPDATE;
  INSERT INTO internal_rpc_authority.authority_snapshot_readbacks (
    workload_id, role, workload_generation, credential_generation,
    source_revision, digest_sha256, key_set_revision, policy_revision,
    signer_generation, served_proof_sha256
  ) VALUES (
    identity.workload_id, identity.role, identity.workload_generation,
    identity.credential_generation, p_source_revision, p_digest_sha256,
    p_key_set_revision, p_policy_revision, p_signer_generation,
    p_served_proof_sha256
  )
  ON CONFLICT (workload_id, role, workload_generation, source_revision)
  DO NOTHING
  RETURNING readback_id INTO result_id;
  IF result_id IS NULL THEN
    SELECT readback_id INTO STRICT result_id
      FROM internal_rpc_authority.authority_snapshot_readbacks
     WHERE workload_id = identity.workload_id
       AND role = identity.role
       AND workload_generation = identity.workload_generation
       AND source_revision = p_source_revision
       AND digest_sha256 = p_digest_sha256
       AND key_set_revision = p_key_set_revision
       AND policy_revision = p_policy_revision
       AND signer_generation = p_signer_generation
       AND served_proof_sha256 = p_served_proof_sha256;
  END IF;
  RETURN result_id;
END
$function$;

ALTER FUNCTION internal_rpc_authority.record_authority_key_delivery_readback(
  bigint, text, bigint, bigint, text
) OWNER TO internal_rpc_authority_readback_owner;
ALTER FUNCTION internal_rpc_authority.record_authority_snapshot_readback(
  bigint, text, bigint, bigint, bigint, text
) OWNER TO internal_rpc_authority_readback_owner;
REVOKE ALL ON FUNCTION internal_rpc_authority.record_authority_key_delivery_readback(
  bigint, text, bigint, bigint, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION internal_rpc_authority.record_authority_snapshot_readback(
  bigint, text, bigint, bigint, bigint, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.record_authority_key_delivery_readback(
  bigint, text, bigint, bigint, text
) TO
  ira_control_api_gateway_issuer_g1,
  ira_control_plane_verifier_g1,
  ira_control_plane_resolver_g1;
GRANT EXECUTE ON FUNCTION internal_rpc_authority.record_authority_snapshot_readback(
  bigint, text, bigint, bigint, bigint, text
) TO
  ira_control_api_gateway_issuer_g1,
  ira_control_plane_verifier_g1,
  ira_control_plane_resolver_g1;

REVOKE ALL ON
  internal_rpc_authority.authority_key_delivery_readbacks,
  internal_rpc_authority.authority_snapshot_readbacks
FROM PUBLIC,
  ira_control_api_gateway_issuer_g1,
  ira_control_plane_verifier_g1,
  ira_control_plane_resolver_g1;
GRANT SELECT ON
  internal_rpc_authority.authority_key_delivery_readbacks,
  internal_rpc_authority.authority_snapshot_readbacks
TO internal_rpc_authority_publisher;

COMMIT;
