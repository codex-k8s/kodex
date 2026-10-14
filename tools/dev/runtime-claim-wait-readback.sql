-- name: RuntimeClaimWaitReadback :one
-- Только read-only диагностика control-plane; SQL text и параметры не выдаются.
SELECT jsonb_build_object(
    'at', clock_timestamp(),
    'sessions', coalesce(jsonb_agg(jsonb_build_object(
        'pid', pid,
        'state', state,
        'waitType', wait_event_type,
        'waitEvent', wait_event,
        'queryFingerprint', md5(query),
        'queryBytes', octet_length(query),
        'namedQuery', coalesce((SELECT tag FROM unnest(ARRAY[
            'runtime_claimexecution_select_claimable_agent_executions',
            'runtime_claimexecution_select_assistant_context',
            'runtime_files_capture_entries', 'runtime_files_capture_catalog',
            'runtime_session_resume_snapshot', 'workflow_launch__lock_projects'
        ]) tag WHERE position(tag IN query) > 0 LIMIT 1), 'OTHER'),
        'relations', ARRAY(SELECT tablename FROM pg_tables
            WHERE schemaname = 'control_plane'
              AND position('control_plane.' || tablename IN query) > 0
            ORDER BY tablename LIMIT 32),
        'stage', CASE
            WHEN query LIKE '-- name: runtime_claimexecution_select_claimable_agent_executions %' THEN 'CANDIDATE_SELECTION'
            WHEN query LIKE '-- name: runtime_claimexecution_select_assistant_context %' THEN 'ASSISTANT_CONTEXT'
            WHEN query LIKE '-- name: workflow_launch__lock_projects %' THEN 'PROJECT_LOCK'
            WHEN query LIKE '-- name: runtime_claimexecution_lock_provider_account %' THEN 'PROVIDER_LOCK'
            WHEN query LIKE '-- name: runtime_session_resume_snapshot %' THEN 'SESSION_SNAPSHOT'
            WHEN query LIKE '-- name: runtime_claimexecution_count_active_provider_leases %' THEN 'PROVIDER_CAPACITY'
            WHEN query LIKE '-- name: runtime_claimexecution_expire_stale_leases %' THEN 'LEASE_EXPIRY'
            ELSE 'OTHER'
        END,
        'transactionAgeSeconds', extract(epoch FROM clock_timestamp() - xact_start),
        'queryAgeSeconds', extract(epoch FROM clock_timestamp() - query_start),
        'blockingPids', pg_blocking_pids(pid)
    ) ORDER BY pid), '[]'::jsonb)
)
FROM pg_stat_activity
WHERE datname = 'control_plane'
  AND pid <> pg_backend_pid()
  AND state <> 'idle';
