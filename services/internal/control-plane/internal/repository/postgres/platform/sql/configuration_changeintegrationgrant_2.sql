-- name: platform__configuration_changeintegrationgrant_2 :one
SELECT capabilities FROM control_plane.integration_definitions WHERE stable_key=$1
