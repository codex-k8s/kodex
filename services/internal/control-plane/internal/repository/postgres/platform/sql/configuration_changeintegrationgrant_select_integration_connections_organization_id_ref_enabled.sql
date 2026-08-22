-- name: platform__configuration_changeintegrationgrant_select_integration_connections_organization_id_ref_enabled :one
SELECT id::text,definition_key,enabled,state,version FROM control_plane.integration_connections WHERE organization_id=$1::uuid AND ref=$2 FOR UPDATE
