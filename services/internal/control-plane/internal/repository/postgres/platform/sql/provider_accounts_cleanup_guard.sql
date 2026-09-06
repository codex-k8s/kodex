-- name: provider_accounts_cleanup_guard :one
SELECT EXISTS (
           SELECT 1
           FROM control_plane.runtime_leases lease
           JOIN control_plane.runtime_revisions revision
             ON revision.id = lease.runtime_revision_id
           WHERE revision.provider_account_id = @account_id::uuid
             AND revision.organization_id = @organization_id::uuid
             AND lease.organization_id = @organization_id::uuid
             AND lease.state = 'CLAIMED'
       ) AS active_runtime_lease,
       EXISTS (
           SELECT 1
           FROM control_plane.assistant_runtime runtime
           JOIN control_plane.sessions session
             ON session.ref = runtime.system_session_ref
            AND session.organization_id = runtime.organization_id
           WHERE runtime.organization_id = @organization_id::uuid
             AND session.provider_account_id = @account_id::uuid
             AND runtime.warm_instance_ref IS NOT NULL
       ) AS active_warm_consumer;
