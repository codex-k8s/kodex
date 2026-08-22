-- name: platform__configuration_changeintegrationgrant_1 :one
SELECT id::text,definition_key FROM control_plane.integration_connections WHERE organization_id=$1::uuid AND ref=$2 AND enabled FOR UPDATE
