-- name: DefinitionInsert
INSERT INTO integration_gateway.definitions (
    definition_id, definition_version, canonical_digest, source, payload, created_at
) VALUES (
    @definition_id, @definition_version, @canonical_digest, @source, @payload::jsonb, @created_at
)
ON CONFLICT (definition_id, definition_version) DO NOTHING
RETURNING definition_id, canonical_digest
