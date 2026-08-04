-- +goose Up
CREATE TABLE control_plane.mattermost_event_verifier_fence (
    producer_id text PRIMARY KEY CHECK (producer_id ~ '^[a-z][a-z0-9.-]{2,127}$'),
    keyset_revision bigint NOT NULL CHECK (keyset_revision >= 0),
    high_watermark bigint NOT NULL CHECK (high_watermark >= 0),
    served_generation bigint NOT NULL CHECK (served_generation >= 0),
    keyset_sha256 text NOT NULL CHECK (keyset_sha256 = '' OR keyset_sha256 ~ '^[0-9a-f]{64}$'),
    retired_generations bigint[] NOT NULL DEFAULT '{}',
    updated_at timestamptz NOT NULL
);
ALTER TABLE control_plane.mattermost_event_verifier_fence OWNER TO control_plane_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.admit_mattermost_event_keyset(
    requested_producer_id text,
    requested_revision bigint,
    requested_high_watermark bigint,
    requested_served_generation bigint,
    requested_keyset_sha256 text,
    requested_retired_generations bigint[],
    requested_active_generations bigint[]
) RETURNS TABLE (
    keyset_revision bigint,
    high_watermark bigint,
    served_generation bigint,
    keyset_sha256 text,
    retired_generations bigint[]
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, control_plane
AS $function$
DECLARE
    fence control_plane.mattermost_event_verifier_fence%ROWTYPE;
    retired_generation bigint;
BEGIN
    IF NOT pg_has_role(session_user, 'control_plane_runtime', 'member')
       OR requested_producer_id !~ '^[a-z][a-z0-9.-]{2,127}$'
       OR requested_revision <= 0 OR requested_high_watermark <= 0
       OR requested_served_generation <> requested_high_watermark
       OR requested_keyset_sha256 !~ '^[0-9a-f]{64}$'
       OR requested_retired_generations IS NULL OR requested_active_generations IS NULL THEN
        RAISE EXCEPTION 'Mattermost event keyset input is invalid' USING ERRCODE = '28000';
    END IF;
    FOREACH retired_generation IN ARRAY requested_retired_generations LOOP
        IF retired_generation <= 0 OR retired_generation >= requested_served_generation THEN
            RAISE EXCEPTION 'Mattermost retired generation is invalid' USING ERRCODE = '28000';
        END IF;
    END LOOP;
    FOREACH retired_generation IN ARRAY requested_active_generations LOOP
        IF retired_generation <= 0 THEN
            RAISE EXCEPTION 'Mattermost active generation is invalid' USING ERRCODE = '28000';
        END IF;
    END LOOP;

    SELECT * INTO fence
      FROM control_plane.mattermost_event_verifier_fence
     WHERE producer_id = requested_producer_id
     FOR UPDATE;
    IF FOUND AND (
        requested_revision < fence.keyset_revision
        OR requested_high_watermark < fence.high_watermark
        OR requested_served_generation < fence.served_generation
        OR requested_high_watermark > fence.high_watermark + 1
        OR (requested_high_watermark > fence.high_watermark
            AND NOT fence.high_watermark = ANY(requested_active_generations))
        OR requested_revision = fence.keyset_revision
           AND requested_keyset_sha256 <> fence.keyset_sha256
        OR fence.retired_generations && requested_active_generations
    ) THEN
        RAISE EXCEPTION 'Mattermost event keyset rollback is forbidden' USING ERRCODE = '28000';
    END IF;
    IF FOUND THEN
        SELECT COALESCE(array_agg(generation ORDER BY generation), '{}')
          INTO requested_retired_generations
          FROM (SELECT DISTINCT unnest(fence.retired_generations || requested_retired_generations) AS generation) AS all_retired;
    END IF;

    INSERT INTO control_plane.mattermost_event_verifier_fence (
        producer_id, keyset_revision, high_watermark, served_generation,
        keyset_sha256, retired_generations, updated_at
    ) VALUES (
        requested_producer_id, requested_revision, requested_high_watermark,
        requested_served_generation, requested_keyset_sha256,
        requested_retired_generations, clock_timestamp()
    )
    ON CONFLICT (producer_id) DO UPDATE SET
        keyset_revision = EXCLUDED.keyset_revision,
        high_watermark = EXCLUDED.high_watermark,
        served_generation = EXCLUDED.served_generation,
        keyset_sha256 = EXCLUDED.keyset_sha256,
        retired_generations = EXCLUDED.retired_generations,
        updated_at = clock_timestamp();

    RETURN QUERY
    SELECT stored.keyset_revision, stored.high_watermark,
           stored.served_generation, stored.keyset_sha256,
           stored.retired_generations
      FROM control_plane.mattermost_event_verifier_fence AS stored
     WHERE stored.producer_id = requested_producer_id;
END
$function$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION control_plane.admit_mattermost_event_keyset(text, bigint, bigint, bigint, text, bigint[], bigint[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.admit_mattermost_event_keyset(text, bigint, bigint, bigint, text, bigint[], bigint[])
    TO control_plane_runtime;
ALTER FUNCTION control_plane.admit_mattermost_event_keyset(text, bigint, bigint, bigint, text, bigint[], bigint[])
    OWNER TO control_plane_owner;

-- +goose Down
-- Forward-only verifier high-watermark и retired generations не удаляются.
SELECT 1;
