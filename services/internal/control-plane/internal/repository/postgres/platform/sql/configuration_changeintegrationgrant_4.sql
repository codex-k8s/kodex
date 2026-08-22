-- name: platform__configuration_changeintegrationgrant_4 :one
UPDATE control_plane.integration_grants SET enabled=false,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND connection_id=$2::uuid AND capability_key=$3 AND target_kind=$4 AND target_ref=$5 RETURNING ref
