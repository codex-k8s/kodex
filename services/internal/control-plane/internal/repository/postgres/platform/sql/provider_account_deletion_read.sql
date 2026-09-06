-- name: provider_account_deletion_read :one
SELECT intent.ref, intent.version, intent.state, intent.safe_reason,
       intent.requested_at, intent.completed_at,
       (SELECT count(*) FROM control_plane.provider_credential_cleanup_tasks task
        WHERE task.provider_account_id = intent.provider_account_id
          AND task.organization_id = intent.organization_id AND task.state <> 'COMPLETED'),
       COALESCE((SELECT jsonb_agg(jsonb_build_object('Kind', counts.kind, 'Total', counts.total) ORDER BY counts.kind)
           FROM (SELECT kind, count(*) AS total
                 FROM control_plane.provider_account_blockers(intent.organization_id, intent.provider_account_id)
                 GROUP BY kind) counts), '[]'::jsonb)
FROM control_plane.provider_account_deletion_intents intent
WHERE intent.organization_id = @organization_id::uuid AND intent.provider_account_id = @account_id::uuid;
