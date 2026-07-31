-- name: RuntimePrincipalBootstrap
SELECT control_plane.bootstrap_runtime_principal($1, $2, $3)
