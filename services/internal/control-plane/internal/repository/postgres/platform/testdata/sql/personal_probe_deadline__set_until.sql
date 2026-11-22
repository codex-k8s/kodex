-- name: personal_probe_deadline__set_until :exec
UPDATE control_plane.access_bindings SET valid_until=$1
WHERE organization_id=$2::uuid AND ref=$3 AND state='ACTIVE'
