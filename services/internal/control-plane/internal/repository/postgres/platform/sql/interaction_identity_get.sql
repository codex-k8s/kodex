-- name: interaction_identity_get :one
SELECT mapping.ref, mapping.version, connection.ref, mapping.connection_version,
       mapping.external_team_ref, mapping.external_channel_ref, mapping.external_user_digest, subject.ref, mapping.state
FROM control_plane.interaction_identities mapping
JOIN control_plane.integration_connections connection ON connection.id=mapping.connection_id
JOIN control_plane.subjects subject ON subject.id=mapping.subject_id
WHERE mapping.organization_id=$1::uuid AND mapping.ref=$2;
