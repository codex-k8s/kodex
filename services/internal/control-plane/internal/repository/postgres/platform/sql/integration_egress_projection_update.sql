-- name: integration_egress_projection_update :one
UPDATE control_plane.integration_egress_projection
SET generation=$1,target_digest=$2,document=$3::jsonb,updated_at=clock_timestamp()
WHERE singleton=true AND generation=$4 RETURNING generation;
