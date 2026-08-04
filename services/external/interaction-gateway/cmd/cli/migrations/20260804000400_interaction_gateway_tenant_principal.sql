-- +goose Up
ALTER TABLE interaction_gateway_runtime_principals DROP CONSTRAINT interaction_gateway_runtime_principals_status_check;
ALTER TABLE interaction_gateway_runtime_principals ADD CONSTRAINT interaction_gateway_runtime_principals_status_check
    CHECK (status IN ('NEXT', 'CURRENT', 'PREVIOUS', 'RETIRED'));

CREATE TABLE interaction_gateway_runtime_principal_authorities (
    principal_name name PRIMARY KEY REFERENCES interaction_gateway_runtime_principals(principal_name),
    generation bigint NOT NULL UNIQUE,
    organization_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (principal_name, generation, organization_id)
);
CREATE TABLE interaction_gateway_runtime_principal_tenants (
    principal_name name NOT NULL,
    generation bigint NOT NULL,
    organization_id uuid NOT NULL,
    project_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (principal_name, project_id),
    FOREIGN KEY (principal_name, generation, organization_id)
        REFERENCES interaction_gateway_runtime_principal_authorities(principal_name, generation, organization_id)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_stage_runtime_identity(
    requested_generation bigint, requested_organization_id uuid, requested_project_ids jsonb
) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
AS $function$
DECLARE requested_principal name := ('interaction_gateway_runtime_g' || requested_generation::text)::name;
        project_text text;
BEGIN
    IF session_user <> 'interaction_gateway_migrator' OR requested_generation <= 0
       OR jsonb_typeof(requested_project_ids) <> 'array' OR jsonb_array_length(requested_project_ids) < 1
       OR NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = requested_principal::text
           AND rolcanlogin AND NOT rolsuper AND NOT rolbypassrls) THEN
        RAISE EXCEPTION 'runtime identity staging input is invalid' USING ERRCODE = '28000';
    END IF;
    IF EXISTS (SELECT 1 FROM interaction_gateway_runtime_principals
        WHERE generation = requested_generation AND status = 'RETIRED') THEN
        RAISE EXCEPTION 'retired runtime identity cannot be restored' USING ERRCODE = '28000';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM interaction_gateway_runtime_principals principal
        JOIN interaction_gateway_runtime_credential_fence fence
          ON fence.singleton AND fence.served_generation = principal.generation
        WHERE principal.principal_name = requested_principal AND principal.generation = requested_generation
          AND principal.status = 'CURRENT') THEN
        INSERT INTO interaction_gateway_runtime_principals (
            principal_name, generation, status, not_before, not_after, updated_at
        ) VALUES (requested_principal, requested_generation, 'NEXT', clock_timestamp() - interval '5 minutes',
            clock_timestamp() + interval '400 days', clock_timestamp())
        ON CONFLICT (principal_name) DO UPDATE SET updated_at = clock_timestamp()
          WHERE interaction_gateway_runtime_principals.generation = requested_generation
            AND interaction_gateway_runtime_principals.status = 'NEXT';
        IF NOT FOUND THEN RAISE EXCEPTION 'runtime identity staging conflict' USING ERRCODE = '28000'; END IF;
    END IF;
    INSERT INTO interaction_gateway_runtime_principal_authorities (
        principal_name, generation, organization_id
    ) VALUES (requested_principal, requested_generation, requested_organization_id)
    ON CONFLICT (principal_name) DO UPDATE SET organization_id = EXCLUDED.organization_id
      WHERE interaction_gateway_runtime_principal_authorities.generation = requested_generation
        AND interaction_gateway_runtime_principal_authorities.organization_id = EXCLUDED.organization_id;
    IF NOT FOUND THEN RAISE EXCEPTION 'runtime organization authority is immutable' USING ERRCODE = '28000'; END IF;
    DELETE FROM interaction_gateway_runtime_principal_tenants
     WHERE principal_name = requested_principal AND generation = requested_generation;
    FOR project_text IN SELECT jsonb_array_elements_text(requested_project_ids) LOOP
        INSERT INTO interaction_gateway_runtime_principal_tenants (
            principal_name, generation, organization_id, project_id
        ) VALUES (requested_principal, requested_generation, requested_organization_id, project_text::uuid);
    END LOOP;
END
$function$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION interaction_gateway_promote_runtime_identity(requested_generation bigint)
RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public
AS $function$
DECLARE requested_principal name := ('interaction_gateway_runtime_g' || requested_generation::text)::name;
        current_generation bigint;
BEGIN
    IF session_user <> 'interaction_gateway_migrator' THEN
        RAISE EXCEPTION 'runtime identity promotion is forbidden' USING ERRCODE = '28000';
    END IF;
    SELECT high_watermark_generation INTO current_generation
      FROM interaction_gateway_runtime_credential_fence WHERE singleton FOR UPDATE;
    IF current_generation IS NOT NULL AND (requested_generation <> current_generation + 1 OR EXISTS (
        SELECT 1 FROM interaction_gateway_runtime_principals WHERE status = 'PREVIOUS')) THEN
        RAISE EXCEPTION 'runtime identity promotion sequence is invalid' USING ERRCODE = '28000';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM interaction_gateway_runtime_principals p
        JOIN interaction_gateway_runtime_principal_tenants t USING (principal_name, generation)
        WHERE p.principal_name = requested_principal AND p.generation = requested_generation
          AND p.status = 'NEXT') THEN
        RAISE EXCEPTION 'staged runtime identity is unavailable' USING ERRCODE = '28000';
    END IF;
    UPDATE interaction_gateway_runtime_principals SET status = 'PREVIOUS', updated_at = clock_timestamp()
     WHERE status = 'CURRENT';
    UPDATE interaction_gateway_runtime_principals SET status = 'CURRENT', updated_at = clock_timestamp()
     WHERE principal_name = requested_principal AND generation = requested_generation AND status = 'NEXT';
    INSERT INTO interaction_gateway_runtime_credential_fence (
        singleton, high_watermark_generation, served_generation, context_key_id, context_key_digest, updated_at
    ) VALUES (true, requested_generation, requested_generation, 'server-owned-principal-scope', repeat('0',64), clock_timestamp())
    ON CONFLICT (singleton) DO UPDATE SET high_watermark_generation = EXCLUDED.high_watermark_generation,
        served_generation = EXCLUDED.served_generation, context_key_id = EXCLUDED.context_key_id,
        context_key_digest = EXCLUDED.context_key_digest, updated_at = clock_timestamp();
    EXECUTE format('GRANT interaction_gateway_runtime TO %I', requested_principal);
END
$function$;
-- +goose StatementEnd

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
     WHERE fence.singleton FOR SHARE;
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

REVOKE ALL ON interaction_gateway_runtime_principal_authorities, interaction_gateway_runtime_principal_tenants
    FROM PUBLIC, interaction_gateway_runtime;
REVOKE ALL ON FUNCTION interaction_gateway_stage_runtime_identity(bigint,uuid,jsonb) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_promote_runtime_identity(bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_activate_runtime_context(uuid,uuid,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION interaction_gateway_runtime_identity_ready(bigint,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION interaction_gateway_activate_runtime_context(uuid,uuid,text) TO interaction_gateway_runtime;
GRANT EXECUTE ON FUNCTION interaction_gateway_runtime_identity_ready(bigint,uuid,jsonb) TO interaction_gateway_runtime;
ALTER FUNCTION interaction_gateway_stage_runtime_identity(bigint,uuid,jsonb) OWNER TO interaction_gateway_role_controller;
ALTER FUNCTION interaction_gateway_promote_runtime_identity(bigint) OWNER TO interaction_gateway_role_controller;
GRANT EXECUTE ON FUNCTION interaction_gateway_stage_runtime_identity(bigint,uuid,jsonb) TO interaction_gateway_migrator;
GRANT EXECUTE ON FUNCTION interaction_gateway_promote_runtime_identity(bigint) TO interaction_gateway_migrator;

-- Старый caller-signed HMAC scope закрыт после перехода на immutable session_user.
REVOKE EXECUTE ON FUNCTION interaction_gateway_activate_runtime_context(uuid,uuid,text,name,bigint,text,uuid,bigint,bytea)
    FROM interaction_gateway_runtime;
REVOKE EXECUTE ON FUNCTION interaction_gateway_security.hmac(bytea,bytea,text) FROM interaction_gateway_runtime;
REVOKE USAGE ON SCHEMA interaction_gateway_security FROM interaction_gateway_runtime;

-- +goose Down
-- Forward-only: principal-to-tenant assignments and retired identities are not removed.
SELECT 1;
