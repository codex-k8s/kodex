-- name: DefinitionGet
SELECT definition_id, canonical_digest
FROM integration_gateway.definitions
WHERE definition_id = @definition_id
  AND definition_version = @definition_version
