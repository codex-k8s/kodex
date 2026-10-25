-- name: repository_bundle__callback_wait :one
SELECT control_plane.repository_bundle_callback_pending(@organization_id::uuid,@run_id::uuid);
