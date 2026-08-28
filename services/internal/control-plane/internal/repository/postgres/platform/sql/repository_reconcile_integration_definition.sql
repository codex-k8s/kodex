-- name: repository_reconcile_integration_definition :one
SELECT definition_version,digest
FROM control_plane.integration_definitions
WHERE stable_key=$1
FOR UPDATE
