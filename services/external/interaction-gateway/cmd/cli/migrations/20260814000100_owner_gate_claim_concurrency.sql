-- +goose Up
RESET ROLE;
SET ROLE interaction_gateway_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_claim_owner_gate_request()
RETURNS uuid
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
DECLARE requested_key uuid;
BEGIN
    IF NOT pg_has_role(session_user, 'interaction_gateway_runtime', 'member') OR NOT EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals AS principal
        JOIN interaction_gateway_runtime_credential_fence AS fence ON fence.singleton
        WHERE principal.principal_name::text = session_user AND principal.status = 'CURRENT'
          AND principal.generation = fence.served_generation
    ) THEN
        RAISE EXCEPTION 'runtime identity is not active' USING ERRCODE = '28000';
    END IF;
    SELECT idempotency_key INTO requested_key
      FROM interaction_gateway_owner_gate_claim_requests
     WHERE state = 'PENDING' ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED;
    IF requested_key IS NULL AND NOT EXISTS (
        SELECT 1 FROM interaction_gateway_delivery_work_scopes WHERE owner_gate_active
    ) THEN
        requested_key := interaction_gateway_security.gen_random_uuid();
        INSERT INTO interaction_gateway_owner_gate_claim_requests(idempotency_key, state)
        VALUES (requested_key, 'PENDING')
        ON CONFLICT DO NOTHING
        RETURNING idempotency_key INTO requested_key;
    END IF;
    RETURN requested_key;
END
$function$;
-- +goose StatementEnd

-- +goose Down
-- Forward-only: restoring the racy claim function is intentionally unsupported.
