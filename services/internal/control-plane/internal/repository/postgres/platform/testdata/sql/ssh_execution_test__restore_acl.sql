-- name: ssh_execution_test__restore_acl :exec
UPDATE control_plane.access_bindings SET state='ACTIVE',version=version+1
WHERE organization_id=$1::uuid AND id=ANY($2::uuid[]);
