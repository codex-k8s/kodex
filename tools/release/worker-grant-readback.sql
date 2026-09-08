-- #1221/#1222: только authoritative metadata, без credential и payload.
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '10s';
SELECT jsonb_build_object(
    'at', clock_timestamp(),
    'activeRuntimeRuns', (SELECT count(*) FROM control_plane.runs
        WHERE state IN ('QUEUED', 'RUNNING', 'CANCELLING')),
    'claimedRuntimeLeases', (SELECT count(*) FROM control_plane.runtime_leases
        WHERE state = 'CLAIMED'),
    'floors', COALESCE((SELECT jsonb_agg(jsonb_build_object(
        'workload', workload_id, 'generation', credential_generation,
        'revision', revision, 'updatedAt', updated_at
    ) ORDER BY workload_id) FROM control_plane.worker_grant_high_watermarks), '[]'::jsonb),
    'instances', COALESCE((SELECT jsonb_agg(jsonb_build_object(
        'workload', workload_id, 'instance', instance_id,
        'generation', credential_generation, 'revision', revision,
        'expiresAt', expires_at, 'updatedAt', updated_at
    ) ORDER BY workload_id, instance_id)
      FROM control_plane.worker_grant_instance_high_watermarks), '[]'::jsonb)
)::text;
COMMIT;
