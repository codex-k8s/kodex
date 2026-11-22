-- name: kubernetes_setup__write_admitted :exec
UPDATE control_plane.kubernetes_identity_setups SET state='ATTEMPTING',version=version+1
WHERE organization_id=@organization_id::uuid AND id=@setup_id::uuid AND state='PREPARED' AND version=@version;
