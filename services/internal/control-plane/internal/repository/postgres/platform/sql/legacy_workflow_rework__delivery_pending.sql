-- name: legacy_workflow_rework__delivery_pending :one
SELECT control_plane.legacy_rework_delivery_pending(@organization_id::uuid,@root_run_id::uuid);
