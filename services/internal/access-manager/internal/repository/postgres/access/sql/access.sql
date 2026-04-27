-- name: organization__create :exec
INSERT INTO access_organizations (
    id, kind, slug, display_name, image_asset_ref, status, parent_organization_id,
    version, created_at, updated_at
) VALUES (
    @id, @kind, @slug, @display_name, @image_asset_ref, @status, @parent_organization_id,
    @version, @created_at, @updated_at
);

-- name: organization__get_by_id :one
SELECT id, kind, slug, display_name, image_asset_ref, status, parent_organization_id, version, created_at, updated_at
FROM access_organizations
WHERE id = @id;

-- name: user__get_by_identity :one
SELECT u.id, u.primary_email, u.display_name, u.avatar_asset_ref, u.status, u.locale, u.version, u.created_at, u.updated_at
FROM access_users u
JOIN access_user_identities i ON i.user_id = u.id
WHERE i.provider = @provider AND i.subject = @subject;

-- name: membership__list_by_subject :many
SELECT id, subject_type, subject_id, target_type, target_id, role_hint, status, source, version, created_at, updated_at
FROM access_memberships
WHERE subject_type = @subject_type AND subject_id = @subject_id AND status = @status;

-- name: access_rule__list_for_check :many
SELECT id, effect, subject_type, subject_id, action_key, resource_type, resource_id, scope_type, scope_id, priority, status, version, created_at, updated_at
FROM access_rules
WHERE action_key = @action_key
  AND resource_type = @resource_type
  AND (resource_id = '' OR resource_id = @resource_id)
  AND (scope_type = '' OR (scope_type = @scope_type AND scope_id = @scope_id))
  AND status = 'active'
ORDER BY priority DESC, updated_at DESC;

-- name: access_decision_audit__create :exec
INSERT INTO access_decision_audit (
    id, subject_type, subject_id, action_key, resource_type, resource_id,
    decision, reason_code, policy_version, explanation, created_at
) VALUES (
    @id, @subject_type, @subject_id, @action_key, @resource_type, @resource_id,
    @decision, @reason_code, @policy_version, @explanation, @created_at
);

-- name: outbox_event__create :exec
INSERT INTO access_outbox_events (
    id, event_type, schema_version, aggregate_type, aggregate_id, payload, occurred_at, published_at
) VALUES (
    @id, @event_type, @schema_version, @aggregate_type, @aggregate_id, @payload, @occurred_at, @published_at
);
