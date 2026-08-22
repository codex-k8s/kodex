-- name: platform__commands_changemembership_2 :one
UPDATE control_plane.memberships SET role=$4,permissions=$5,active=$6,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2 AND version=$3 RETURNING ref,role,permissions,active,version
