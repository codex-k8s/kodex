-- +goose Up
RESET ROLE;
SET ROLE interaction_gateway_owner;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_bind_owner_gate_request(
    requested_key uuid, requested_gate_id uuid, requested_delivery_id uuid
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
DECLARE
    active_organization uuid;
    active_project uuid;
    delivery_organization uuid;
    delivery_project uuid;
    existing_state text;
    existing_gate_id uuid;
    existing_delivery_id uuid;
BEGIN
    SELECT organization_id, project_id INTO active_organization, active_project
      FROM interaction_gateway_runtime_scope();
    SELECT organization_id, project_id INTO delivery_organization, delivery_project
      FROM interaction_gateway_delivery_work_scopes WHERE delivery_id = requested_delivery_id;
    IF (active_organization, active_project) IS DISTINCT FROM (delivery_organization, delivery_project) THEN
        RAISE EXCEPTION 'owner gate delivery scope mismatch' USING ERRCODE = '28000';
    END IF;

    SELECT request.state, request.owner_gate_id, request.delivery_id
      INTO existing_state, existing_gate_id, existing_delivery_id
      FROM interaction_gateway_owner_gate_claim_requests AS request
     WHERE request.idempotency_key = requested_key
     FOR UPDATE;
    IF NOT FOUND THEN
        RETURN false;
    END IF;
    IF existing_state = 'CLAIMED' THEN
        RETURN existing_gate_id = requested_gate_id AND existing_delivery_id = requested_delivery_id;
    END IF;
    IF existing_state <> 'PENDING' THEN
        RETURN false;
    END IF;

    UPDATE interaction_gateway_owner_gate_claim_requests SET
        state = 'CLAIMED', owner_gate_id = requested_gate_id, delivery_id = requested_delivery_id,
        updated_at = clock_timestamp()
    WHERE idempotency_key = requested_key AND state = 'PENDING';
    RETURN FOUND;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_complete_owner_gate_request(requested_key uuid)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $function$
DECLARE
    existing_state text;
    bound_delivery uuid;
    active_organization uuid;
    active_project uuid;
    delivery_organization uuid;
    delivery_project uuid;
BEGIN
    IF NOT pg_has_role(session_user, 'interaction_gateway_runtime', 'member') OR NOT EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals AS principal
        JOIN interaction_gateway_runtime_credential_fence AS fence ON fence.singleton
        WHERE principal.principal_name::text = session_user AND principal.status = 'CURRENT'
          AND principal.generation = fence.served_generation
    ) THEN
        RAISE EXCEPTION 'runtime identity is not active' USING ERRCODE = '28000';
    END IF;

    SELECT request.state, request.delivery_id INTO existing_state, bound_delivery
      FROM interaction_gateway_owner_gate_claim_requests AS request
     WHERE request.idempotency_key = requested_key
     FOR UPDATE;
    IF NOT FOUND THEN
        RETURN false;
    END IF;
    IF existing_state = 'COMPLETED' THEN
        RETURN true;
    END IF;
    IF existing_state NOT IN ('PENDING', 'CLAIMED') THEN
        RETURN false;
    END IF;

    IF bound_delivery IS NOT NULL THEN
        SELECT organization_id, project_id INTO active_organization, active_project
          FROM interaction_gateway_runtime_scope();
        SELECT organization_id, project_id INTO delivery_organization, delivery_project
          FROM interaction_gateway_delivery_work_scopes WHERE delivery_id = bound_delivery;
        IF (active_organization, active_project) IS DISTINCT FROM (delivery_organization, delivery_project) THEN
            RAISE EXCEPTION 'owner gate completion scope mismatch' USING ERRCODE = '28000';
        END IF;
    END IF;

    UPDATE interaction_gateway_owner_gate_claim_requests SET
        state = 'COMPLETED', updated_at = clock_timestamp()
    WHERE idempotency_key = requested_key AND state IN ('PENDING', 'CLAIMED');
    RETURN FOUND;
END
$function$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION interaction_gateway_bind_owner_gate_request(uuid, uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_complete_owner_gate_request(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION interaction_gateway_bind_owner_gate_request(uuid, uuid, uuid)
    TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_complete_owner_gate_request(uuid)
    TO interaction_gateway_runtime;

RESET ROLE;

-- +goose Down
-- Forward-only: restoring non-idempotent owner-gate replay is intentionally unsupported.
