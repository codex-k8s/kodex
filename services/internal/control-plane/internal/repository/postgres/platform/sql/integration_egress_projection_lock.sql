-- name: integration_egress_projection_lock :one
SELECT generation,target_digest,document
FROM control_plane.integration_egress_projection WHERE singleton=true FOR UPDATE;
