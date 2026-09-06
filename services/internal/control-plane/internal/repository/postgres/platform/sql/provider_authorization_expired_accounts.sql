-- name: provider_authorization_expired_accounts :many
SELECT account.id::text
FROM control_plane.provider_accounts account
WHERE EXISTS (SELECT 1 FROM control_plane.provider_authorization_attempts attempt
              WHERE attempt.provider_account_id=account.id AND attempt.preparation_state='RESERVED'
                AND attempt.reservation_deadline<=clock_timestamp())
ORDER BY account.id LIMIT @limit FOR UPDATE OF account SKIP LOCKED;
