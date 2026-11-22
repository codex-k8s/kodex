-- name: email_connection_test__setup :one
SELECT control_plane.email_health_input_snapshot(
  @organization_id::uuid,@connection_id::uuid,@actor_id::uuid) IS NOT NULL;
