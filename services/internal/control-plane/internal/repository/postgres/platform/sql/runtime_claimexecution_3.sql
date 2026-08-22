-- name: platform__runtime_claimexecution_3 :one
SELECT COALESCE(max(generation),0)+1 FROM control_plane.runtime_leases WHERE node_id=$1::uuid
