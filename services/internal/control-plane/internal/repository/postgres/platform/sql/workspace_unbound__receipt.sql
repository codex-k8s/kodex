-- name: workspace_unbound__receipt :one
SELECT record FROM control_plane.workspace_unbound_stops
WHERE organization_id=@organization_id::uuid AND lease_id=@lease_id::uuid;
