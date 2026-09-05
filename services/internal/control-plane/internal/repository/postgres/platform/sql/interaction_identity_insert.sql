-- name: interaction_identity_insert :one
INSERT INTO control_plane.interaction_identities
(ref, organization_id, connection_id, connection_version, external_team_ref, external_channel_ref, external_user_digest, subject_id, created_by)
SELECT @ref, @organization_id::uuid, @connection_id::uuid, @connection_version,
       @team_ref, @channel_ref, @user_digest, subject.id, @actor_id::uuid
FROM control_plane.subjects subject
JOIN control_plane.memberships membership ON membership.subject_id=subject.id
 AND membership.organization_id=subject.organization_id AND membership.project_id IS NULL AND membership.active
WHERE subject.organization_id=@organization_id::uuid AND subject.ref=@subject_ref AND subject.kind='USER' AND subject.active
RETURNING ref, version;
