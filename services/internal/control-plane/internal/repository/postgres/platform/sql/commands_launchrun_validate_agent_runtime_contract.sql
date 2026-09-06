-- name: commands_launchrun_validate_agent_runtime_contract :one
SELECT control_plane.agent_runtime_contract_ready(@organization_id::uuid, @project_id::uuid,
 @agent_refs::text[], @role_runtime_contract_revision, @role_runtime_contract_sha256);
