-- +goose Up
RESET ROLE;
SET ROLE interaction_gateway_owner;

-- interaction-gateway обслуживает динамические Project одного проверенного
-- Organization. Точный Project по-прежнему приходит из authority context или
-- из server-owned Mattermost route, но rotation identity больше не требует
-- заранее перечислять все будущие Project UUID.
ALTER TABLE interaction_gateway_runtime_principal_authorities
    ADD COLUMN all_projects boolean NOT NULL DEFAULT true;

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
    PERFORM 1
      FROM interaction_gateway_runtime_principal_authorities AS authority
     WHERE authority.principal_name::text = session_user
       AND authority.generation = served_generation
       AND authority.organization_id = requested_organization_id
       AND (
           authority.all_projects
           OR EXISTS (
               SELECT 1
                 FROM interaction_gateway_runtime_principal_tenants AS tenant
                WHERE tenant.principal_name = authority.principal_name
                  AND tenant.generation = authority.generation
                  AND tenant.organization_id = authority.organization_id
                  AND tenant.project_id = requested_project_id
           )
       );
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

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_runtime_identity_ready(
    requested_generation bigint, requested_organization_id uuid, requested_project_ids jsonb
) RETURNS boolean
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = pg_catalog, public
AS $function$
DECLARE requested_principal name := ('interaction_gateway_runtime_g' || requested_generation::text)::name;
BEGIN
    RETURN requested_principal::text = session_user
       AND jsonb_typeof(requested_project_ids) = 'array'
       AND EXISTS (SELECT 1 FROM interaction_gateway_runtime_credential_fence fence
          JOIN interaction_gateway_runtime_principals principal
            ON principal.generation = fence.served_generation AND principal.status = 'CURRENT'
           AND principal.principal_name = requested_principal
          JOIN interaction_gateway_runtime_principal_authorities authority
            ON authority.principal_name = principal.principal_name
           AND authority.generation = principal.generation
           AND authority.organization_id = requested_organization_id
           AND authority.all_projects
          JOIN pg_roles role ON role.rolname = principal.principal_name AND role.rolcanlogin
           AND NOT role.rolsuper AND NOT role.rolbypassrls
         WHERE fence.singleton AND fence.served_generation = requested_generation)
       AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements_text(requested_project_ids) project
          WHERE NOT EXISTS (SELECT 1 FROM interaction_gateway_runtime_principal_tenants assigned
             WHERE assigned.principal_name = requested_principal AND assigned.generation = requested_generation
               AND assigned.organization_id = requested_organization_id AND assigned.project_id = project::uuid))
       AND (SELECT count(*) FROM interaction_gateway_runtime_principal_tenants
          WHERE principal_name = requested_principal AND generation = requested_generation)
           = jsonb_array_length(requested_project_ids);
END
$function$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION interaction_gateway_activate_runtime_context(uuid,uuid,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_runtime_identity_ready(bigint,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION interaction_gateway_activate_runtime_context(uuid,uuid,text) TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_runtime_identity_ready(bigint,uuid,jsonb) TO interaction_gateway_runtime;

RESET ROLE;

-- +goose Down
-- Forward-only: динамический project scope является частью owner boundary.
SELECT 1;
