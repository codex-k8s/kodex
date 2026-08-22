-- name: platform__runtime_claimexecution_select_runtime_leases_node_id :one
SELECT COALESCE(max(generation),0)+1 FROM control_plane.runtime_leases WHERE node_id=$1::uuid
