-- name: assistant_configuration_component_effects :one
SELECT jsonb_build_array(
 (SELECT count(*) FROM control_plane.audit_events WHERE organization_id=$1::uuid),
 (SELECT count(*) FROM control_plane.idempotency_receipts WHERE organization_id=$1::uuid),
 (SELECT count(*) FROM control_plane.role_image_recipes WHERE organization_id=$1::uuid),
 (SELECT count(*) FROM control_plane.image_builds WHERE organization_id=$1::uuid),
 (SELECT count(*) FROM control_plane.outbox_events))::text;
