-- name: platform__bootstrap_component_readback :one
SELECT
    (SELECT count(*) FROM control_plane.organizations) AS organization_count,
    (SELECT count(*) FROM control_plane.owner_claim_contracts
        WHERE stable_key = 'installation-owner' AND state = 'PENDING_CLAIM') AS owner_contract_count,
    (SELECT count(*) FROM control_plane.agents
        WHERE system_key = 'system-assistant' AND project_id IS NULL
          AND enabled AND state = 'READY') AS system_assistant_count,
    (SELECT count(*) FROM control_plane.instruction_versions
        WHERE core AND state = 'PUBLISHED') AS core_prompt_count,
    (SELECT count(*) FROM control_plane.assistant_runtime
        WHERE stable_key = 'system-assistant' AND runtime_state = 'STARTING'
          AND system_session_ref <> '') AS assistant_runtime_count,
    (SELECT count(*) FROM control_plane.platform_capabilities) AS capability_count,
    (SELECT count(*) FROM control_plane.integration_definitions
        WHERE optional AND enabled) AS integration_definition_count,
    (SELECT count(*) FROM control_plane.installation
        WHERE singleton AND bootstrapped_at IS NOT NULL) AS completed_bootstrap_count;
