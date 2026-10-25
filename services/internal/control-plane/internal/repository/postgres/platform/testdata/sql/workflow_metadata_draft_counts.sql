-- name: workflow_metadata_draft_counts :one
SELECT
 (SELECT count(*) FROM control_plane.workflows WHERE project_id=(SELECT id FROM control_plane.projects WHERE ref=$1)),
 (SELECT count(*) FROM control_plane.idempotency_receipts WHERE idempotency_key=$2),
 (SELECT count(*) FROM control_plane.audit_events WHERE project_id=(SELECT id FROM control_plane.projects WHERE ref=$1) AND resource_kind='WORKFLOW'),
 (SELECT count(*) FROM control_plane.outbox_events WHERE convert_from(payload,'UTF8')::jsonb->>'projectRef'=$1 AND convert_from(payload,'UTF8')::jsonb->'data'->>'kind'='WORKFLOW'),
 (SELECT count(*) FROM control_plane.runs WHERE project_id=(SELECT id FROM control_plane.projects WHERE ref=$1))
