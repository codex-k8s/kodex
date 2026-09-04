-- name: configuration_changeintegrationgrant_select_integration_definitions_stable_key :one
SELECT capabilities FROM control_plane.integration_definitions
WHERE stable_key=$1
  AND (adapter_owner,execution_route) IN
      (('integration-gateway','MANAGED_MCP'),('interaction-gateway','INTERACTION'))
  AND adapter_readiness='READY'
