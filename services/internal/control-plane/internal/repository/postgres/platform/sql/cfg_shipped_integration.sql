-- name: cfg_shipped_integration :one
SELECT version,definition_version,digest,origin FROM control_plane.integration_definitions
WHERE stable_key=$1 AND enabled FOR SHARE
