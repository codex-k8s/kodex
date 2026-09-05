-- name: configuration_changeconnection_select_integration_definitions_stable_key_enabled :one
SELECT definition_version,digest FROM control_plane.integration_definitions
WHERE stable_key=$1 AND enabled
  AND (adapter_owner,execution_route) IN
      (('integration-gateway','MANAGED_MCP'),('interaction-gateway','INTERACTION'))
  AND adapter_readiness='READY'
