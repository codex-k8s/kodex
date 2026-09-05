-- name: configuration_writeback__list :many
SELECT work.ref FROM control_plane.managed_configuration_writebacks work
WHERE work.organization_id=$1::uuid AND work.configuration_set_id=$2::uuid
  AND ($3='' OR work.ref>$3)
ORDER BY work.ref LIMIT $4;
