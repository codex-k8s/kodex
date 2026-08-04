-- name: ConnectionUpsert
INSERT INTO integration_gateway.connections (
    connection_id, tenant_id, project_id, integration_id, revision, generation,
    status, definition_id, definition_version, payload, updated_at
) VALUES (
    @connection_id, @tenant_id, @project_id, @integration_id, @revision, @generation,
    @status, @definition_id, @definition_version, @payload::jsonb, clock_timestamp()
)
ON CONFLICT (connection_id) DO UPDATE SET
    revision = GREATEST(integration_gateway.connections.revision, EXCLUDED.revision),
    generation = GREATEST(integration_gateway.connections.generation, EXCLUDED.generation),
    status = CASE WHEN EXCLUDED.generation > integration_gateway.connections.generation
                       OR EXCLUDED.revision > integration_gateway.connections.revision
                  THEN EXCLUDED.status ELSE integration_gateway.connections.status END,
    definition_id = CASE WHEN EXCLUDED.generation > integration_gateway.connections.generation
                              OR EXCLUDED.revision > integration_gateway.connections.revision
                         THEN EXCLUDED.definition_id ELSE integration_gateway.connections.definition_id END,
    definition_version = CASE WHEN EXCLUDED.generation > integration_gateway.connections.generation
                                   OR EXCLUDED.revision > integration_gateway.connections.revision
                              THEN EXCLUDED.definition_version ELSE integration_gateway.connections.definition_version END,
    payload = CASE WHEN EXCLUDED.generation > integration_gateway.connections.generation
                        OR EXCLUDED.revision > integration_gateway.connections.revision
                   THEN EXCLUDED.payload ELSE integration_gateway.connections.payload END,
    updated_at = CASE WHEN EXCLUDED.generation > integration_gateway.connections.generation
                           OR EXCLUDED.revision > integration_gateway.connections.revision
                      THEN clock_timestamp() ELSE integration_gateway.connections.updated_at END
WHERE integration_gateway.connections.tenant_id = EXCLUDED.tenant_id
  AND integration_gateway.connections.project_id = EXCLUDED.project_id
  AND integration_gateway.connections.integration_id = EXCLUDED.integration_id
  AND EXCLUDED.generation >= integration_gateway.connections.generation
  AND EXCLUDED.revision >= integration_gateway.connections.revision
RETURNING connection_id
