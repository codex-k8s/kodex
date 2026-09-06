-- name: configuration_writeback__count :one
SELECT count(*) FROM control_plane.managed_configuration_writebacks
WHERE organization_id=$1::uuid AND configuration_set_id=$2::uuid;
