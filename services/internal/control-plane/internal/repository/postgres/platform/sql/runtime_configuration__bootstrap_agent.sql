-- name: runtime_configuration__bootstrap_agent :one
WITH eligible_accounts AS (
    SELECT provider_account.ref
    FROM control_plane.provider_accounts provider_account
    WHERE provider_account.organization_id = @organization_id::uuid
      AND provider_account.definition_key = @provider
      AND provider_account.enabled
      AND provider_account.state = 'AUTHORIZED'
      AND provider_account.current_credential_revision_id IS NOT NULL
    ORDER BY provider_account.ref
    LIMIT 32
), account_pool AS (
    SELECT jsonb_agg(
               jsonb_build_object('accountRef', eligible_accounts.ref, 'weight', 1)
               ORDER BY eligible_accounts.ref
           ) AS candidates,
           '[' || string_agg(
               format('{"accountRef":"%s","weight":1}', eligible_accounts.ref),
               ',' ORDER BY eligible_accounts.ref
           ) || ']' AS canonical_candidates,
           count(*) AS account_count
    FROM eligible_accounts
), policy AS (
    INSERT INTO control_plane.provider_account_policy_versions
        (ref, organization_id, agent_id, version_number, mode, account_candidates, digest, created_by)
    SELECT @policy_ref, @organization_id::uuid, @agent_id::uuid, 1,
           CASE WHEN account_pool.account_count = 1 THEN 'FIXED' ELSE 'LEAST_USED' END,
           account_pool.candidates,
           encode(digest(
               convert_to(CASE WHEN account_pool.account_count = 1 THEN 'FIXED' ELSE 'LEAST_USED' END, 'UTF8') ||
               decode('00', 'hex') || convert_to(account_pool.canonical_candidates, 'UTF8') || decode('00', 'hex'),
               'sha256'
           ), 'hex'),
           @created_by::uuid
    FROM account_pool
    WHERE account_pool.account_count > 0
    RETURNING id, ref, digest
), config AS (
    INSERT INTO control_plane.agent_runtime_config_versions
        (ref, organization_id, agent_id, version_number, provider_account_policy_id,
         runtime_profile_key, provider, model, digest, created_by)
    SELECT @config_ref, @organization_id::uuid, @agent_id::uuid, 1, policy.id,
           @runtime_profile_ref, @provider, @model,
           encode(digest(convert_to(@runtime_profile_ref, 'UTF8') || decode('00', 'hex') ||
                         convert_to(@provider, 'UTF8') || decode('00', 'hex') ||
                         convert_to(@model, 'UTF8') || decode('00', 'hex') ||
                         convert_to(policy.ref, 'UTF8') || decode('00', 'hex') ||
                         convert_to('1', 'UTF8') || decode('00', 'hex') ||
                         convert_to(policy.digest, 'UTF8') || decode('00', 'hex'), 'sha256'), 'hex'),
           @created_by::uuid
    FROM policy
    RETURNING id
), overlay_version AS (
    INSERT INTO control_plane.agent_config_overlay_versions
        (ref, organization_id, agent_id, version_number, state, content, digest,
         validation_errors, created_by, validated_at, published_at)
    VALUES (@overlay_ref, @organization_id::uuid, @agent_id::uuid, 1, 'PUBLISHED', '',
            'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
            '[]'::jsonb, @created_by::uuid, clock_timestamp(), clock_timestamp())
    RETURNING id
), inserted_environment AS (
    INSERT INTO control_plane.runtime_environment_sets
        (ref, organization_id, project_id, name, description, created_by)
    VALUES (@environment_ref, @organization_id::uuid, NULLIF(@project_id, '')::uuid,
            'i18n:DEFAULT_RUNTIME_ENVIRONMENT', 'i18n:DEFAULT_RUNTIME_ENVIRONMENT_DESCRIPTION', @created_by::uuid)
    ON CONFLICT ON CONSTRAINT runtime_environment_sets_organization_id_project_id_name_key DO NOTHING
    RETURNING id, ref, current_version_id
), environment AS (
    SELECT id, ref, current_version_id FROM inserted_environment
    UNION ALL
    SELECT existing.id, existing.ref, existing.current_version_id
    FROM control_plane.runtime_environment_sets existing
    WHERE existing.organization_id = @organization_id::uuid
      AND existing.project_id IS NOT DISTINCT FROM NULLIF(@project_id, '')::uuid
      AND existing.name = 'i18n:DEFAULT_RUNTIME_ENVIRONMENT'
    LIMIT 1
), current_environment_version AS (
    SELECT current_version.id,
           current_version.version_number,
           current_version.role_image_artifact_id
    FROM environment
    LEFT JOIN control_plane.runtime_environment_versions current_version
      ON current_version.id = environment.current_version_id
), inserted_environment_version AS (
    INSERT INTO control_plane.runtime_environment_versions
        (ref, organization_id, environment_set_id, version_number, non_secret_values,
         secret_descriptors, role_image_artifact_id, selected_tools, digest, created_by)
    SELECT @environment_version_ref, @organization_id::uuid, environment.id,
           COALESCE(current_environment_version.version_number, 0) + 1,
           '[]'::jsonb, '[]'::jsonb, NULLIF(@environment_image_artifact_id, '')::uuid,
           @environment_selected_tools, @environment_digest, @created_by::uuid
    FROM environment
    JOIN current_environment_version ON true
    WHERE (environment.current_version_id IS NULL OR current_environment_version.role_image_artifact_id IS NULL)
      AND (@project_id = '' OR NULLIF(@environment_image_artifact_id, '') IS NOT NULL)
    RETURNING id, environment_set_id
), binding AS (
    INSERT INTO control_plane.agent_runtime_environment_bindings
        (ref, organization_id, agent_id, environment_set_id, digest, updated_by)
    SELECT @binding_ref, @organization_id::uuid, @agent_id::uuid, environment.id,
           encode(digest(convert_to(agent.ref, 'UTF8') || decode('00', 'hex') ||
                         convert_to(environment.ref, 'UTF8') || decode('00', 'hex') ||
                         convert_to('1', 'UTF8') || decode('00', 'hex'), 'sha256'), 'hex'),
           @created_by::uuid
    FROM environment
    JOIN control_plane.agents agent ON agent.id = @agent_id::uuid
    RETURNING id
), updated_agent AS (
    UPDATE control_plane.agents agent
    SET current_runtime_config_id = config.id,
        current_config_overlay_id = overlay_version.id
    FROM config, overlay_version, binding
    WHERE agent.id = @agent_id::uuid
    RETURNING agent.id
)
SELECT updated_agent.id::text,
       environment.id::text,
       COALESCE(inserted_environment_version.id, environment.current_version_id)::text
FROM updated_agent
JOIN environment ON true
LEFT JOIN inserted_environment_version
  ON inserted_environment_version.environment_set_id = environment.id;
