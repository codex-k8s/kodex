-- name: platform__configuration_addassistantturncommand_1 :one
SELECT runtime_state IN('READY','BUSY') AND runtime_revision=desired_runtime_revision AND last_heartbeat_at>clock_timestamp()-interval '45 seconds' FROM control_plane.assistant_runtime WHERE organization_id=$1::uuid
