-- name: bootstrap_component_warm_heartbeat_counts :one
SELECT (SELECT count(*)
        FROM control_plane.audit_events
        WHERE action = 'controlplane.report_warm_runtime') AS audit_count,
       (SELECT count(*)
        FROM control_plane.outbox_events
        WHERE subject LIKE 'control_plane.platform.%') AS outbox_count;
