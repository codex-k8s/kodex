-- name: provider_account_deletions_read :many
SELECT account.ref, intent.ref, intent.version, intent.state, intent.safe_reason,
       intent.requested_at, intent.completed_at,
       (SELECT count(*) FROM control_plane.provider_credential_cleanup_tasks task
        WHERE task.provider_account_id = intent.provider_account_id
          AND task.organization_id = intent.organization_id AND task.state <> 'COMPLETED'),
       COALESCE((SELECT jsonb_agg(jsonb_build_object('Kind', counts.kind, 'Total', counts.total) ORDER BY counts.kind)
           FROM (SELECT kind, count(*) AS total
                 FROM control_plane.provider_account_blockers(intent.organization_id, intent.provider_account_id)
                 GROUP BY kind) counts), '[]'::jsonb)
FROM control_plane.provider_account_deletion_intents intent
JOIN control_plane.provider_accounts account
  ON account.id = intent.provider_account_id AND account.organization_id = intent.organization_id
WHERE intent.organization_id = @organization_id::uuid AND account.ref = ANY(@account_refs::text[]);
