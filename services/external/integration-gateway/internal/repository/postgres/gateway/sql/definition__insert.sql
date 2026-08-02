-- name: DefinitionInsert
INSERT INTO integration_gateway.definitions (
    definition_id, definition_version, canonical_digest, source, payload, created_at
) VALUES (
    @definition_id, @definition_version, @canonical_digest, @source, @payload::jsonb, @created_at
)
ON CONFLICT (definition_id, definition_version) DO UPDATE
SET canonical_digest = integration_gateway.definitions.canonical_digest
WHERE integration_gateway.definitions.canonical_digest = EXCLUDED.canonical_digest
RETURNING definition_id
