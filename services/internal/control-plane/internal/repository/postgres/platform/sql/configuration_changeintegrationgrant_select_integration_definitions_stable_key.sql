-- name: configuration_changeintegrationgrant_select_integration_definitions_stable_key :one
SELECT capabilities FROM control_plane.integration_definitions
WHERE stable_key=$1
  AND adapter_owner='integration-gateway'
  AND execution_route='MANAGED_MCP'
  AND adapter_readiness='READY'
