-- +goose Up
RESET ROLE;
SET ROLE interaction_gateway_owner;
CREATE TABLE interaction_gateway_delivery_readback_keyset_fence (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    keyset_revision bigint NOT NULL CHECK (keyset_revision > 0),
    high_watermark bigint NOT NULL CHECK (high_watermark > 0),
    served_generation bigint NOT NULL CHECK (served_generation > 0 AND served_generation <= high_watermark),
    keyset_sha256 text NOT NULL CHECK (keyset_sha256 ~ '^[0-9a-f]{64}$'),
    retired_generations bigint[] NOT NULL DEFAULT '{}',
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE interaction_gateway_delivery_readback_key_history (
    generation bigint PRIMARY KEY CHECK (generation > 0),
    kid text NOT NULL UNIQUE CHECK (length(kid) BETWEEN 1 AND 128),
    thumbprint_sha256 text NOT NULL UNIQUE CHECK (thumbprint_sha256 ~ '^[0-9a-f]{64}$'),
    status text NOT NULL CHECK (status IN ('CURRENT','NEXT','PREVIOUS','RETIRED')),
    first_revision bigint NOT NULL CHECK (first_revision > 0),
    last_revision bigint NOT NULL CHECK (last_revision >= first_revision),
    retired_at timestamptz,
    CHECK ((status = 'RETIRED') = (retired_at IS NOT NULL))
);
CREATE TABLE interaction_gateway_delivery_readback_keyset_audit (
    id uuid PRIMARY KEY,
    action text NOT NULL CHECK (action IN ('GENESIS','ADMIT')),
    keyset_revision bigint NOT NULL,
    keyset_sha256 text NOT NULL CHECK (keyset_sha256 ~ '^[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_bootstrap_delivery_readback_keyset_genesis(
    requested_revision bigint, requested_high_watermark bigint, requested_served_generation bigint,
    requested_digest text, requested_identities jsonb
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
AS $function$
DECLARE identity_row jsonb;
BEGIN
    IF session_user <> 'interaction_gateway_migrator' OR requested_revision <> 1 OR
       requested_high_watermark <> requested_served_generation OR requested_served_generation <= 0 OR
       requested_digest !~ '^[0-9a-f]{64}$' OR jsonb_typeof(requested_identities) <> 'array' OR
       jsonb_array_length(requested_identities) < 1 OR jsonb_array_length(requested_identities) > 4 THEN
        RAISE EXCEPTION 'delivery readback keyset genesis is invalid' USING ERRCODE = '28000';
    END IF;
    INSERT INTO interaction_gateway_delivery_readback_keyset_fence
        (singleton,keyset_revision,high_watermark,served_generation,keyset_sha256)
    VALUES (true,requested_revision,requested_high_watermark,requested_served_generation,requested_digest);
    FOR identity_row IN SELECT value FROM jsonb_array_elements(requested_identities) LOOP
        IF (identity_row->>'generation')::bigint <= 0 OR
           identity_row->>'status' NOT IN ('CURRENT','NEXT','PREVIOUS','RETIRED') OR
           identity_row->>'kid' = '' OR identity_row->>'thumbprint_sha256' !~ '^[0-9a-f]{64}$' OR
           ((identity_row->>'status' = 'CURRENT') <> ((identity_row->>'generation')::bigint = requested_served_generation)) THEN
            RAISE EXCEPTION 'delivery readback key identity genesis is invalid' USING ERRCODE = '28000';
        END IF;
        INSERT INTO interaction_gateway_delivery_readback_key_history
            (generation,kid,thumbprint_sha256,status,first_revision,last_revision,retired_at)
        VALUES ((identity_row->>'generation')::bigint,identity_row->>'kid',identity_row->>'thumbprint_sha256',
            identity_row->>'status',requested_revision,requested_revision,
            CASE WHEN identity_row->>'status' = 'RETIRED' THEN clock_timestamp() END);
    END LOOP;
    IF (SELECT count(*) FROM interaction_gateway_delivery_readback_key_history WHERE status='CURRENT') <> 1 THEN
        RAISE EXCEPTION 'delivery readback keyset genesis CURRENT identity is invalid' USING ERRCODE = '28000';
    END IF;
    INSERT INTO interaction_gateway_delivery_readback_keyset_audit(id,action,keyset_revision,keyset_sha256)
    VALUES (gen_random_uuid(),'GENESIS',requested_revision,requested_digest);
EXCEPTION WHEN unique_violation THEN
    RAISE EXCEPTION 'delivery readback keyset genesis already exists' USING ERRCODE = '55000';
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_admit_delivery_readback_keyset(
    requested_revision bigint, requested_high_watermark bigint, requested_served_generation bigint,
    requested_digest text, requested_identities jsonb
) RETURNS TABLE(keyset_revision bigint, high_watermark bigint, served_generation bigint,
    keyset_sha256 text, retired_generations bigint[])
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
AS $function$
DECLARE current_fence interaction_gateway_delivery_readback_keyset_fence%ROWTYPE;
DECLARE identity_row jsonb;
DECLARE existing interaction_gateway_delivery_readback_key_history%ROWTYPE;
DECLARE requested_status text;
BEGIN
    SELECT * INTO current_fence FROM interaction_gateway_delivery_readback_keyset_fence WHERE singleton FOR UPDATE;
    IF NOT FOUND OR NOT EXISTS (SELECT 1 FROM interaction_gateway_delivery_readback_keyset_audit WHERE action='GENESIS') THEN
        RAISE EXCEPTION 'delivery readback keyset genesis is required' USING ERRCODE = '55000';
    END IF;
    IF requested_digest !~ '^[0-9a-f]{64}$' OR jsonb_typeof(requested_identities) <> 'array' OR
       jsonb_array_length(requested_identities) < 1 OR jsonb_array_length(requested_identities) > 4 OR
       requested_revision < current_fence.keyset_revision OR requested_high_watermark < current_fence.high_watermark OR
       requested_served_generation < current_fence.served_generation OR
       requested_served_generation <> requested_high_watermark OR requested_high_watermark > current_fence.high_watermark + 1 OR
       (requested_revision = current_fence.keyset_revision AND requested_digest <> current_fence.keyset_sha256) THEN
        RAISE EXCEPTION 'delivery readback keyset rollback is forbidden' USING ERRCODE = '28000';
    END IF;
    FOR identity_row IN SELECT value FROM jsonb_array_elements(requested_identities) LOOP
        requested_status := identity_row->>'status';
        IF (identity_row->>'generation')::bigint <= 0 OR
           requested_status NOT IN ('CURRENT','NEXT','PREVIOUS','RETIRED') OR
           identity_row->>'kid' = '' OR identity_row->>'thumbprint_sha256' !~ '^[0-9a-f]{64}$' OR
           ((requested_status = 'CURRENT') <> ((identity_row->>'generation')::bigint = requested_served_generation)) THEN
            RAISE EXCEPTION 'delivery readback key identity is invalid' USING ERRCODE = '28000';
        END IF;
        SELECT * INTO existing FROM interaction_gateway_delivery_readback_key_history
         WHERE generation=(identity_row->>'generation')::bigint;
        IF FOUND AND (existing.kid <> identity_row->>'kid' OR existing.thumbprint_sha256 <> identity_row->>'thumbprint_sha256' OR
           existing.status = 'RETIRED' AND requested_status <> 'RETIRED' OR
           existing.status = 'PREVIOUS' AND requested_status IN ('NEXT','CURRENT') OR
           existing.status = 'CURRENT' AND requested_status = 'NEXT') THEN
            RAISE EXCEPTION 'delivery readback key identity rollback is forbidden' USING ERRCODE = '28000';
        END IF;
        IF NOT FOUND AND (identity_row->>'generation')::bigint <= current_fence.high_watermark THEN
            RAISE EXCEPTION 'delivery readback key identity history is incomplete' USING ERRCODE = '28000';
        END IF;
        INSERT INTO interaction_gateway_delivery_readback_key_history
            (generation,kid,thumbprint_sha256,status,first_revision,last_revision,retired_at)
        VALUES ((identity_row->>'generation')::bigint,identity_row->>'kid',identity_row->>'thumbprint_sha256',
            requested_status,requested_revision,requested_revision,
            CASE WHEN requested_status='RETIRED' THEN clock_timestamp() END)
        ON CONFLICT (generation) DO UPDATE SET status=EXCLUDED.status,last_revision=EXCLUDED.last_revision,
            retired_at=COALESCE(interaction_gateway_delivery_readback_key_history.retired_at,EXCLUDED.retired_at);
    END LOOP;
    UPDATE interaction_gateway_delivery_readback_key_history AS history
       SET status='RETIRED', retired_at=COALESCE(retired_at,clock_timestamp()),
           last_revision=requested_revision
     WHERE history.status<>'RETIRED' AND NOT EXISTS(
       SELECT 1 FROM jsonb_array_elements(requested_identities) i
        WHERE (i->>'generation')::bigint=history.generation);
    IF EXISTS (SELECT 1 FROM interaction_gateway_delivery_readback_key_history h
               WHERE h.status='RETIRED' AND NOT EXISTS (
                   SELECT 1 FROM jsonb_array_elements(requested_identities) i
                    WHERE (i->>'generation')::bigint=h.generation AND i->>'status'='RETIRED')) THEN
        RAISE EXCEPTION 'retired delivery readback key disappeared' USING ERRCODE = '28000';
    END IF;
    IF (SELECT count(*) FROM interaction_gateway_delivery_readback_key_history WHERE status='CURRENT') <> 1 THEN
        RAISE EXCEPTION 'delivery readback keyset CURRENT identity is invalid' USING ERRCODE = '28000';
    END IF;
    UPDATE interaction_gateway_delivery_readback_keyset_fence SET
        keyset_revision=requested_revision,high_watermark=requested_high_watermark,
        served_generation=requested_served_generation,keyset_sha256=requested_digest,
        retired_generations=ARRAY(SELECT generation FROM interaction_gateway_delivery_readback_key_history WHERE status='RETIRED' ORDER BY generation),
        updated_at=clock_timestamp() WHERE singleton;
    IF requested_revision > current_fence.keyset_revision THEN
        INSERT INTO interaction_gateway_delivery_readback_keyset_audit(id,action,keyset_revision,keyset_sha256)
        VALUES (gen_random_uuid(),'ADMIT',requested_revision,requested_digest);
    END IF;
    RETURN QUERY SELECT f.keyset_revision,f.high_watermark,f.served_generation,f.keyset_sha256,f.retired_generations
      FROM interaction_gateway_delivery_readback_keyset_fence f WHERE singleton;
END
$function$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION interaction_gateway_bootstrap_delivery_readback_keyset_genesis(bigint,bigint,bigint,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION interaction_gateway_bootstrap_delivery_readback_keyset_genesis(bigint,bigint,bigint,text,jsonb) TO interaction_gateway_migrator;
REVOKE ALL ON FUNCTION interaction_gateway_admit_delivery_readback_keyset(bigint,bigint,bigint,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION interaction_gateway_admit_delivery_readback_keyset(bigint,bigint,bigint,text,jsonb) TO interaction_gateway_runtime;

-- +goose Down
SELECT 1;
