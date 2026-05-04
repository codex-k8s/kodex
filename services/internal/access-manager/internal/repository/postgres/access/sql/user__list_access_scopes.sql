-- name: user__list_access_scopes :many
WITH scoped_memberships AS (
    SELECT 'organization' AS scope_type, target_id::text AS scope_id
    FROM access_memberships
    WHERE subject_type = 'user'
      AND subject_id = @user_id
      AND target_type = 'organization'
      AND status IN ('active', 'pending', 'blocked')
),
scoped_allowlist AS (
    SELECT 'organization' AS scope_type, entry.organization_id::text AS scope_id
    FROM access_users AS usr
    JOIN access_allowlist_entries AS entry
      ON entry.status = 'active'
     AND entry.organization_id IS NOT NULL
     AND (
        (entry.match_type = 'email' AND entry.value = usr.primary_email) OR
        (entry.match_type = 'domain' AND usr.primary_email LIKE ('%@' || entry.value))
     )
    WHERE usr.id = @user_id
)
SELECT scope_type, scope_id
FROM (
    SELECT scope_type, scope_id FROM scoped_memberships
    UNION
    SELECT scope_type, scope_id FROM scoped_allowlist
) AS scopes
ORDER BY scope_type, scope_id;
