-- name: secret_draft_owner_scope :one
SELECT subject.ref,organization.ref FROM control_plane.subjects subject
JOIN control_plane.organizations organization ON organization.id=subject.organization_id
WHERE subject.organization_id=@organization_id::uuid AND subject.id=@actor_id::uuid AND subject.active;
