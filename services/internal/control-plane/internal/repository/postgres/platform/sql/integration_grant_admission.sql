-- name: integration_grant_admission :one
SELECT connection_version, definition_key, definition_version, definition_digest,
       project_ref, recipient_version, reason
FROM control_plane.integration_grant_admission(
    @organization_id::uuid, @actor_id::uuid, NULLIF(@authority_project_id,'')::uuid,
    @connection_ref, '', @recipient_kind, @recipient_ref, @capability_key, 'GRANT', NULL
)
