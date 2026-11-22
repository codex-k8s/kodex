-- name: kubernetes_capture_acl_bound :exec
UPDATE control_plane.access_bindings SET valid_until=$2 WHERE organization_id=$1::uuid AND state='ACTIVE';
