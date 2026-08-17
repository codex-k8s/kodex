-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_retire_runtime_identity(requested_generation bigint)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $function$
DECLARE
    requested_principal name := ('interaction_gateway_runtime_g' || requested_generation::text)::name;
    current_high_watermark bigint;
BEGIN
    IF session_user <> 'interaction_gateway_migrator' THEN
        RAISE EXCEPTION 'runtime identity retirement is forbidden' USING ERRCODE = '28000';
    END IF;
    SELECT high_watermark_generation INTO STRICT current_high_watermark
      FROM interaction_gateway_runtime_credential_fence WHERE singleton FOR UPDATE;
    IF requested_generation >= current_high_watermark OR NOT EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals
         WHERE generation = requested_generation
    ) THEN
        RAISE EXCEPTION 'runtime identity retirement input is invalid' USING ERRCODE = '28000';
    END IF;
    UPDATE interaction_gateway_runtime_principals
       SET status = 'RETIRED', updated_at = clock_timestamp()
     WHERE generation = requested_generation AND status <> 'RETIRED';
    EXECUTE format('ALTER ROLE %I NOLOGIN', requested_principal);
    EXECUTE format('REVOKE interaction_gateway_runtime FROM %I', requested_principal);
    PERFORM pg_terminate_backend(pid) FROM pg_catalog.pg_stat_activity
     WHERE usename = requested_principal::text AND pid <> pg_backend_pid();
END
$function$;
-- +goose StatementEnd

-- +goose Down
-- Forward-only: повторный retirement не отменяет credential fence и retired identity.
SELECT 1;
