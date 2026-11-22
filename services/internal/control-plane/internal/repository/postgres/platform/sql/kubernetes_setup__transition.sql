-- name: kubernetes_setup__transition :exec
UPDATE control_plane.kubernetes_identity_setups SET state=@state,version=version+1
WHERE id=@setup_id::uuid AND organization_id=@organization_id::uuid
 AND state IN('PREPARED','ATTEMPTING','UNKNOWN') AND state<>@state;
