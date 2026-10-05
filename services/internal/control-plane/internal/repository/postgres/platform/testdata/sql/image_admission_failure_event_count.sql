-- name: image_admission_failure_event_count :one
SELECT count(*) FROM control_plane.outbox_events
WHERE convert_from(payload, 'UTF8')::jsonb->>'eventName' = 'ROLE_IMAGE_RECIPE_CHANGED'
  AND convert_from(payload, 'UTF8')::jsonb->>'aggregateRef' = $1
  AND convert_from(payload, 'UTF8')::jsonb->>'organizationRef' = $2
  AND convert_from(payload, 'UTF8')::jsonb->'data'->>'state' = $3;
