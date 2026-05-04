-- name: pending_access__list :many
WITH pending_items AS (
    SELECT
        id::text AS item_id,
        'user' AS item_type,
        'user' AS subject_type,
        id::text AS subject_id,
        status,
        ('user_' || status) AS reason_code,
        created_at,
        updated_at AS sort_at
    FROM access_users AS usr
    WHERE status IN ('pending', 'blocked')
      AND (
        @scope_type = '' OR
        @scope_type = 'global' OR
        (
          @scope_type = 'organization' AND (
            EXISTS (
              SELECT 1
              FROM access_memberships AS membership
              WHERE membership.subject_type = 'user'
                AND membership.subject_id = usr.id
                AND membership.target_type = 'organization'
                AND membership.target_id::text = @scope_id
                AND membership.status IN ('active', 'pending', 'blocked')
            ) OR
            EXISTS (
              SELECT 1
              FROM access_allowlist_entries AS entry
              WHERE entry.status = 'active'
                AND entry.organization_id::text = @scope_id
                AND (
                  (entry.match_type = 'email' AND entry.value = usr.primary_email) OR
                  (entry.match_type = 'domain' AND usr.primary_email LIKE ('%@' || entry.value))
                )
            )
          )
        )
      )

    UNION ALL

    SELECT
        id::text AS item_id,
        'membership' AS item_type,
        subject_type,
        subject_id::text AS subject_id,
        status,
        ('membership_' || status) AS reason_code,
        created_at,
        updated_at AS sort_at
    FROM access_memberships
    WHERE status IN ('pending', 'blocked')
      AND (
        @scope_type = '' OR
        @scope_type = 'global' OR
        (target_type = @scope_type AND target_id::text = @scope_id)
      )

    UNION ALL

    SELECT
        id::text AS item_id,
        'external_account' AS item_type,
        'external_account' AS subject_type,
        id::text AS subject_id,
        CASE
            WHEN status = 'blocked' THEN 'blocked'
            ELSE 'pending'
        END AS status,
        ('external_account_' || status) AS reason_code,
        created_at,
        updated_at AS sort_at
    FROM access_external_accounts
    WHERE status IN ('pending', 'needs_reauth', 'limited', 'blocked')
      AND (
        @scope_type = '' OR
        @scope_type = 'global' OR
        (owner_scope_type = @scope_type AND owner_scope_id = @scope_id)
      )
)
SELECT
    item_id,
    item_type,
    subject_type,
    subject_id,
    status,
    reason_code,
    created_at
FROM pending_items
ORDER BY sort_at DESC, item_type, item_id
LIMIT @limit OFFSET @offset;
