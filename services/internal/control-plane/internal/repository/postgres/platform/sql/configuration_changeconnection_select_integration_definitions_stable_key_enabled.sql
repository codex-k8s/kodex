-- name: configuration_changeconnection_select_integration_definitions_stable_key_enabled :one
SELECT definition_version,digest FROM control_plane.integration_definitions
WHERE stable_key=$1 AND enabled
  AND adapter_owner='integration-gateway'
  AND execution_route='MANAGED_MCP'
  AND adapter_readiness='READY'
