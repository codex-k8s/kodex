-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_activate_runtime_context(
    requested_organization_id uuid, requested_project_id uuid, requested_actor_id text
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
AS $function$
DECLARE served_generation bigint;
BEGIN
    IF requested_actor_id = '' OR NOT pg_has_role(session_user, 'interaction_gateway_runtime', 'member') THEN
        RAISE EXCEPTION 'runtime context identity is invalid' USING ERRCODE = '28000';
    END IF;
    SELECT fence.served_generation INTO STRICT served_generation
      FROM interaction_gateway_runtime_credential_fence fence
      JOIN interaction_gateway_runtime_principals principal
        ON principal.generation = fence.served_generation AND principal.principal_name::text = session_user
       AND principal.status = 'CURRENT' AND clock_timestamp() >= principal.not_before
       AND clock_timestamp() < principal.not_after
      JOIN pg_roles role ON role.rolname = principal.principal_name AND role.rolcanlogin
       AND NOT role.rolsuper AND NOT role.rolbypassrls
     WHERE fence.singleton FOR SHARE OF fence, principal;
    PERFORM 1 FROM interaction_gateway_runtime_principal_tenants
     WHERE principal_name::text = session_user AND generation = served_generation
       AND organization_id = requested_organization_id AND project_id = requested_project_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'runtime tenant scope is not assigned to session principal' USING ERRCODE = '28000';
    END IF;
    INSERT INTO interaction_gateway_runtime_transaction_contexts (
        backend_pid, transaction_id, principal_name, principal_generation,
        organization_id, project_id, actor_id, nonce, expires_at, created_at
    ) VALUES (pg_backend_pid(), txid_current(), session_user::name, served_generation,
        requested_organization_id, requested_project_id, requested_actor_id,
        gen_random_uuid(), clock_timestamp() + interval '30 seconds', clock_timestamp());
END
$function$;
-- +goose StatementEnd
