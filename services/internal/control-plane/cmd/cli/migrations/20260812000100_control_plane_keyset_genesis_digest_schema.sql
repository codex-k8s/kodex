-- +goose Up
RESET ROLE;
SET ROLE control_plane_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.bootstrap_mattermost_event_keyset_genesis(
    requested_producer_id text, requested_revision bigint, requested_high_watermark bigint,
    requested_served_generation bigint, requested_keyset_sha256 text, requested_key_identities jsonb
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, control_plane, control_plane_extensions
AS $function$
DECLARE identity_row record; identity_digest text;
BEGIN
    IF session_user <> 'control_plane_migrator' OR requested_revision <= 0 OR requested_high_watermark <= 0
       OR requested_served_generation <> requested_high_watermark
       OR requested_keyset_sha256 !~ '^[0-9a-f]{64}$' OR jsonb_typeof(requested_key_identities) <> 'array'
       OR jsonb_array_length(requested_key_identities) < 1 OR jsonb_array_length(requested_key_identities) > 4 THEN
        RAISE EXCEPTION 'Mattermost event keyset genesis is invalid' USING ERRCODE = '28000';
    END IF;
    identity_digest := encode(control_plane_extensions.digest(
        convert_to(requested_key_identities::text, 'UTF8'), 'sha256'
    ), 'hex');
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

REVOKE ALL ON FUNCTION control_plane.bootstrap_mattermost_event_keyset_genesis(text,bigint,bigint,bigint,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.bootstrap_mattermost_event_keyset_genesis(text,bigint,bigint,bigint,text,jsonb) TO control_plane_migrator;

-- +goose Down
-- Forward-only: production genesis may depend on the corrected extension schema.
SELECT 1;
