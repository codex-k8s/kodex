-- +goose Up
RESET ROLE;
SET ROLE integration_gateway_owner;
CREATE TABLE integration_gateway.result_grant_verifier_fence (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    keyset_revision bigint NOT NULL CHECK (keyset_revision >= 0),
    high_watermark bigint NOT NULL CHECK (high_watermark >= 0),
    served_generation bigint NOT NULL CHECK (served_generation >= 0),
    keyset_sha256 text NOT NULL CHECK (keyset_sha256 = '' OR keyset_sha256 ~ '^[0-9a-f]{64}$'),
    updated_at timestamptz NOT NULL
);
INSERT INTO integration_gateway.result_grant_verifier_fence (
    singleton, keyset_revision, high_watermark, served_generation, keyset_sha256, updated_at
) VALUES (true, 0, 0, 0, '', clock_timestamp());
ALTER TABLE integration_gateway.result_grant_verifier_fence OWNER TO integration_gateway_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION integration_gateway.admit_result_grant_keyset(
    requested_revision bigint,
    requested_high_watermark bigint,
    requested_served_generation bigint,
    requested_keyset_sha256 text,
    token_signer_generation bigint
) RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, integration_gateway
AS $function$
DECLARE
    fence integration_gateway.result_grant_verifier_fence%ROWTYPE;
BEGIN
    IF NOT pg_has_role(session_user, 'integration_gateway_runtime', 'member')
       OR requested_revision <= 0 OR requested_high_watermark <= 0
       OR requested_served_generation <> requested_high_watermark
       OR requested_keyset_sha256 !~ '^[0-9a-f]{64}$'
       OR token_signer_generation <= 0
       OR token_signer_generation > requested_served_generation
       OR token_signer_generation + 1 < requested_served_generation THEN
        RAISE EXCEPTION 'result grant keyset input is invalid' USING ERRCODE = '28000';
    END IF;
    SELECT * INTO STRICT fence
      FROM integration_gateway.result_grant_verifier_fence
     WHERE singleton FOR UPDATE;
    IF requested_revision < fence.keyset_revision
       OR requested_high_watermark < fence.high_watermark
       OR requested_served_generation < fence.served_generation
       OR requested_revision = fence.keyset_revision
          AND fence.keyset_sha256 <> ''
          AND requested_keyset_sha256 <> fence.keyset_sha256 THEN
        RAISE EXCEPTION 'result grant keyset rollback is forbidden' USING ERRCODE = '28000';
    END IF;
    UPDATE integration_gateway.result_grant_verifier_fence SET
        keyset_revision = requested_revision,
        high_watermark = requested_high_watermark,
        served_generation = requested_served_generation,
        keyset_sha256 = requested_keyset_sha256,
        updated_at = clock_timestamp()
     WHERE singleton;
    RETURN true;
END
$function$;
-- +goose StatementEnd

GRANT EXECUTE ON FUNCTION integration_gateway.admit_result_grant_keyset(bigint, bigint, bigint, text, bigint)
    TO integration_gateway_runtime;
ALTER FUNCTION integration_gateway.admit_result_grant_keyset(bigint, bigint, bigint, text, bigint)
    OWNER TO integration_gateway_owner;

-- +goose Down
-- Forward-only verifier high-watermark не удаляется и не уменьшается.
SELECT 1;
