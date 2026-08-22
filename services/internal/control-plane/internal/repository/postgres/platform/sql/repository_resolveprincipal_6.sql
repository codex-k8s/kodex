-- name: platform__repository_resolveprincipal_6 :exec
UPDATE control_plane.organizations SET authority_tenant_ref=$2 WHERE id=$1::uuid
