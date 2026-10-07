-- name: system_assistant_integration_grants__connection :one
SELECT connection.version, connection.definition_key, connection.definition_version, connection.definition_digest,
       connection.enabled AND connection.state='CONNECTED' AND connection.lifecycle_state='ACTIVE'
       AND definition.enabled AND definition.adapter_readiness='READY'
       AND (definition.adapter_owner,definition.execution_route) IN
           (('integration-gateway','MANAGED_MCP'),('interaction-gateway','INTERACTION'))
       AND (definition.credential_secret_key IS NULL OR EXISTS (
           SELECT 1 FROM control_plane.integration_credential_revisions credential
           WHERE credential.id=connection.credential_revision_id
             AND credential.organization_id=connection.organization_id AND credential.connection_id=connection.id
             AND connection.masked_credentials_state='CONFIGURED'
       )) AS ready
FROM control_plane.integration_connections connection
JOIN control_plane.integration_definitions definition ON definition.stable_key=connection.definition_key
WHERE connection.organization_id=$1::uuid AND connection.ref=$2 AND connection.lifecycle_state='ACTIVE';
