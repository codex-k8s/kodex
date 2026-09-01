-- name: workers_reconcilewarmruntime_insert_session :one
INSERT INTO control_plane.sessions
    (ref, organization_id, project_id, target_type, target_ref,
     provider_account_id, state, created_by)
VALUES
    (@session_ref, @organization_id::uuid, NULL, 'SYSTEM_ASSISTANT',
     'system-assistant', @provider_account_id::uuid, 'ACTIVE', @created_by::uuid)
RETURNING id::text;
