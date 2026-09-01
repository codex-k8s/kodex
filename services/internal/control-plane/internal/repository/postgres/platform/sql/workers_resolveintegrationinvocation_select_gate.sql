-- name: workers_resolveintegrationinvocation_select_gate :one
SELECT ref FROM control_plane.owner_gates WHERE integration_invocation_id=$1::uuid
