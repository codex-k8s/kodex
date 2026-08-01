-- name: RuntimePrincipalsReconcile
SELECT control_plane.reconcile_runtime_principals(
    $1::jsonb,
    $2::text,
    $3::bytea
)
