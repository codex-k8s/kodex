-- name: accessgraph__ensure_bootstrap_owner_organization_membership :exec
WITH owner_org AS (
    INSERT INTO organizations (slug, name)
    VALUES ($1, $2)
    ON CONFLICT (slug) DO UPDATE
    SET name = EXCLUDED.name,
        updated_at = NOW()
    RETURNING id
), clear_prev_owner AS (
    UPDATE organization_memberships
    SET role = 'admin',
        updated_at = NOW()
    WHERE organization_id = (SELECT id FROM owner_org)
      AND role = 'owner'
      AND user_id <> $3
)
INSERT INTO organization_memberships (organization_id, user_id, role)
SELECT id, $3, $4
FROM owner_org
ON CONFLICT (organization_id, user_id) DO UPDATE
SET role = EXCLUDED.role,
    updated_at = NOW();
