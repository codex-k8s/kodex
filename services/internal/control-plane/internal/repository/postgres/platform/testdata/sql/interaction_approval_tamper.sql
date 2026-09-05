-- name: interaction_approval_tamper :exec
UPDATE control_plane.interaction_deliveries SET connection_version=connection_version+1
WHERE ref=$1;
