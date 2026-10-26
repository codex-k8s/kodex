-- name: ssh_compose__secret_lock :one
SELECT ref,purpose FROM control_plane.runtime_secrets
WHERE organization_id=@organization_id::uuid AND project_id=@project_id::uuid
  AND scope_kind='PROJECT' AND ref=@secret_ref AND state='ACTIVE'
FOR UPDATE NOWAIT;
