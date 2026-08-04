-- +goose Up
CREATE TABLE control_plane.mattermost_event_key_history (
    producer_id text NOT NULL REFERENCES control_plane.mattermost_event_verifier_fence(producer_id),
    generation bigint NOT NULL CHECK (generation > 0),
    kid text NOT NULL CHECK (length(kid) BETWEEN 1 AND 128),
    thumbprint_sha256 text NOT NULL CHECK (thumbprint_sha256 ~ '^[0-9a-f]{64}$'),
    status text NOT NULL CHECK (status IN ('NEXT', 'CURRENT', 'PREVIOUS', 'RETIRED')),
    first_revision bigint NOT NULL CHECK (first_revision > 0),
    last_revision bigint NOT NULL CHECK (last_revision >= first_revision),
    retired_at timestamptz,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (producer_id, generation),
    UNIQUE (producer_id, kid),
    UNIQUE (producer_id, thumbprint_sha256),
    CHECK ((status = 'RETIRED') = (retired_at IS NOT NULL))
);
ALTER TABLE control_plane.mattermost_event_key_history OWNER TO control_plane_owner;

CREATE TABLE control_plane.mattermost_event_keyset_genesis_audit (
    producer_id text PRIMARY KEY,
    keyset_revision bigint NOT NULL,
    keyset_sha256 text NOT NULL CHECK (keyset_sha256 ~ '^[0-9a-f]{64}$'),
    key_identities_sha256 text NOT NULL CHECK (key_identities_sha256 ~ '^[0-9a-f]{64}$'),
    controller_principal name NOT NULL,
    created_at timestamptz NOT NULL
);
ALTER TABLE control_plane.mattermost_event_keyset_genesis_audit OWNER TO control_plane_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.bootstrap_mattermost_event_keyset_genesis(
    requested_producer_id text, requested_revision bigint, requested_high_watermark bigint,
    requested_served_generation bigint, requested_keyset_sha256 text, requested_key_identities jsonb
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, control_plane, public
AS $function$
DECLARE identity_row record; identity_digest text;
BEGIN
    IF session_user <> 'control_plane_migrator' OR requested_revision <= 0 OR requested_high_watermark <= 0
       OR requested_served_generation <> requested_high_watermark
       OR requested_keyset_sha256 !~ '^[0-9a-f]{64}$' OR jsonb_typeof(requested_key_identities) <> 'array'
       OR jsonb_array_length(requested_key_identities) < 1 OR jsonb_array_length(requested_key_identities) > 4 THEN
        RAISE EXCEPTION 'Mattermost event keyset genesis is invalid' USING ERRCODE = '28000';
    END IF;
    identity_digest := encode(public.digest(convert_to(requested_key_identities::text, 'UTF8'), 'sha256'), 'hex');
    INSERT INTO control_plane.mattermost_event_verifier_fence (
        producer_id, keyset_revision, high_watermark, served_generation, keyset_sha256, retired_generations, updated_at
    ) VALUES (requested_producer_id, requested_revision, requested_high_watermark,
        requested_served_generation, requested_keyset_sha256, '{}', clock_timestamp());
    FOR identity_row IN SELECT * FROM jsonb_to_recordset(requested_key_identities)
      AS entry(generation bigint, status text, kid text, thumbprint_sha256 text)
    LOOP
        IF identity_row.generation <= 0 OR identity_row.status NOT IN ('NEXT','CURRENT','PREVIOUS','RETIRED')
           OR identity_row.kid = '' OR identity_row.thumbprint_sha256 !~ '^[0-9a-f]{64}$'
           OR (identity_row.status = 'CURRENT') <> (identity_row.generation = requested_served_generation) THEN
            RAISE EXCEPTION 'Mattermost event key identity genesis is invalid' USING ERRCODE = '28000';
        END IF;
        INSERT INTO control_plane.mattermost_event_key_history (
            producer_id, generation, kid, thumbprint_sha256, status, first_revision, last_revision,
            retired_at, updated_at
        ) VALUES (requested_producer_id, identity_row.generation, identity_row.kid,
            identity_row.thumbprint_sha256, identity_row.status, requested_revision, requested_revision,
            CASE WHEN identity_row.status = 'RETIRED' THEN clock_timestamp() END, clock_timestamp());
    END LOOP;
    IF (SELECT count(*) FROM control_plane.mattermost_event_key_history
        WHERE producer_id = requested_producer_id AND status = 'CURRENT') <> 1 THEN
        RAISE EXCEPTION 'Mattermost event keyset genesis CURRENT identity is invalid' USING ERRCODE = '28000';
    END IF;
    INSERT INTO control_plane.mattermost_event_keyset_genesis_audit (
        producer_id, keyset_revision, keyset_sha256, key_identities_sha256, controller_principal, created_at
    ) VALUES (requested_producer_id, requested_revision, requested_keyset_sha256,
        identity_digest, session_user, clock_timestamp());
EXCEPTION WHEN unique_violation THEN
    RAISE EXCEPTION 'Mattermost event keyset genesis already exists' USING ERRCODE = '28000';
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.admit_mattermost_event_keyset(
    requested_producer_id text, requested_revision bigint, requested_high_watermark bigint,
    requested_served_generation bigint, requested_keyset_sha256 text,
    requested_retired_generations bigint[], requested_active_generations bigint[], requested_key_identities jsonb
) RETURNS TABLE (keyset_revision bigint, high_watermark bigint, served_generation bigint,
    keyset_sha256 text, retired_generations bigint[])
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, control_plane
AS $function$
DECLARE fence control_plane.mattermost_event_verifier_fence%ROWTYPE; identity_row record; stored record;
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_runtime', 'member') OR requested_revision <= 0
       OR requested_high_watermark <= 0 OR requested_served_generation <> requested_high_watermark
       OR requested_keyset_sha256 !~ '^[0-9a-f]{64}$' OR jsonb_typeof(requested_key_identities) <> 'array' THEN
        RAISE EXCEPTION 'Mattermost event keyset input is invalid' USING ERRCODE = '28000';
    END IF;
    SELECT * INTO fence FROM control_plane.mattermost_event_verifier_fence
     WHERE producer_id = requested_producer_id FOR UPDATE;
    IF NOT FOUND OR NOT EXISTS (SELECT 1 FROM control_plane.mattermost_event_keyset_genesis_audit
       WHERE producer_id = requested_producer_id) THEN
        RAISE EXCEPTION 'Mattermost event keyset genesis is unavailable' USING ERRCODE = '28000';
    END IF;
    IF requested_revision < fence.keyset_revision OR requested_high_watermark < fence.high_watermark
       OR requested_served_generation < fence.served_generation OR requested_high_watermark > fence.high_watermark + 1
       OR (requested_revision = fence.keyset_revision AND requested_keyset_sha256 <> fence.keyset_sha256) THEN
        RAISE EXCEPTION 'Mattermost event keyset rollback is forbidden' USING ERRCODE = '28000';
    END IF;
    FOR identity_row IN SELECT * FROM jsonb_to_recordset(requested_key_identities)
      AS entry(generation bigint, status text, kid text, thumbprint_sha256 text)
    LOOP
        SELECT * INTO stored FROM control_plane.mattermost_event_key_history
         WHERE producer_id = requested_producer_id AND generation = identity_row.generation FOR UPDATE;
        IF FOUND AND (stored.kid <> identity_row.kid OR stored.thumbprint_sha256 <> identity_row.thumbprint_sha256
          OR stored.status = 'RETIRED' AND identity_row.status <> 'RETIRED'
          OR stored.status = 'PREVIOUS' AND identity_row.status IN ('NEXT','CURRENT')
          OR stored.status = 'CURRENT' AND identity_row.status = 'NEXT') THEN
            RAISE EXCEPTION 'Mattermost event key identity rollback is forbidden' USING ERRCODE = '28000';
        END IF;
        IF NOT FOUND AND identity_row.generation <= fence.high_watermark THEN
            RAISE EXCEPTION 'Mattermost event key identity history is incomplete' USING ERRCODE = '28000';
        END IF;
        INSERT INTO control_plane.mattermost_event_key_history (
            producer_id, generation, kid, thumbprint_sha256, status, first_revision, last_revision, retired_at, updated_at
        ) VALUES (requested_producer_id, identity_row.generation, identity_row.kid, identity_row.thumbprint_sha256,
            identity_row.status, requested_revision, requested_revision,
            CASE WHEN identity_row.status = 'RETIRED' THEN clock_timestamp() END, clock_timestamp())
        ON CONFLICT (producer_id, generation) DO UPDATE SET status = EXCLUDED.status,
            last_revision = EXCLUDED.last_revision,
            retired_at = CASE WHEN EXCLUDED.status = 'RETIRED'
                THEN COALESCE(control_plane.mattermost_event_key_history.retired_at, clock_timestamp()) END,
            updated_at = clock_timestamp();
    END LOOP;
    UPDATE control_plane.mattermost_event_key_history AS history
       SET status = 'RETIRED', retired_at = COALESCE(retired_at, clock_timestamp()),
           last_revision = requested_revision, updated_at = clock_timestamp()
     WHERE history.producer_id = requested_producer_id AND history.status <> 'RETIRED'
       AND NOT EXISTS (SELECT 1 FROM jsonb_to_recordset(requested_key_identities)
          AS entry(generation bigint, status text, kid text, thumbprint_sha256 text)
          WHERE entry.generation = history.generation);
    IF EXISTS (SELECT 1 FROM control_plane.mattermost_event_key_history
       WHERE producer_id = requested_producer_id AND status = 'RETIRED'
         AND generation = ANY(requested_active_generations))
       OR (SELECT count(*) FROM control_plane.mattermost_event_key_history
           WHERE producer_id = requested_producer_id AND status = 'CURRENT') <> 1 THEN
        RAISE EXCEPTION 'Mattermost event keyset active history is invalid' USING ERRCODE = '28000';
    END IF;
    SELECT COALESCE(array_agg(generation ORDER BY generation), '{}') INTO requested_retired_generations
      FROM control_plane.mattermost_event_key_history
     WHERE producer_id = requested_producer_id AND status = 'RETIRED';
    UPDATE control_plane.mattermost_event_verifier_fence SET keyset_revision = requested_revision,
        high_watermark = requested_high_watermark, served_generation = requested_served_generation,
        keyset_sha256 = requested_keyset_sha256, retired_generations = requested_retired_generations,
        updated_at = clock_timestamp() WHERE producer_id = requested_producer_id;
    RETURN QUERY SELECT requested_revision, requested_high_watermark, requested_served_generation,
        requested_keyset_sha256, requested_retired_generations;
END
$function$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION control_plane.bootstrap_mattermost_event_keyset_genesis(text,bigint,bigint,bigint,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.bootstrap_mattermost_event_keyset_genesis(text,bigint,bigint,bigint,text,jsonb) TO control_plane_migrator;
REVOKE ALL ON FUNCTION control_plane.admit_mattermost_event_keyset(text,bigint,bigint,bigint,text,bigint[],bigint[],jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.admit_mattermost_event_keyset(text,bigint,bigint,bigint,text,bigint[],bigint[],jsonb) TO control_plane_runtime;

-- +goose Down
-- Forward-only: verifier key identity/history and retired union are not removed.
SELECT 1;
