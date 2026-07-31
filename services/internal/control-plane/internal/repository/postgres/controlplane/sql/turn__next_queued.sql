-- name: TurnNextQueued
WITH candidate AS (
    SELECT queued.*
    FROM control_plane.resources AS queued
    WHERE queued.organization_id = @organization_id::uuid
      AND queued.project_id = @project_id::uuid
      AND queued.kind = 'TURN'
      AND queued.state = 'QUEUED'
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.resources AS earlier
          WHERE earlier.organization_id = queued.organization_id
            AND earlier.project_id = queued.project_id
            AND earlier.kind = 'TURN'
            AND earlier.spec ->> 'sessionId' = queued.spec ->> 'sessionId'
            AND (earlier.spec ->> 'sequence')::bigint
                < (queued.spec ->> 'sequence')::bigint
            AND earlier.state = 'QUEUED'
      )
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.resources AS active
          WHERE active.organization_id = queued.organization_id
            AND active.project_id = queued.project_id
            AND active.kind = 'TURN'
            AND active.spec ->> 'sessionId' = queued.spec ->> 'sessionId'
            AND active.state IN (
                'CLAIMED',
                'RUNNING',
                'WAITING_OWNER',
                'WAITING_EXTERNAL',
                'BLOCKED'
            )
      )
    ORDER BY queued.created_at, queued.id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
),
serialized AS (
    SELECT
        candidate.*,
        pg_advisory_xact_lock(
            hashtextextended(candidate.spec ->> 'sessionId', 0)
        ) AS locked
    FROM candidate
)
SELECT
    id::text,
    organization_id::text,
    project_id::text,
    coalesce(parent_id::text, ''),
    owner_actor_id::text,
    kind,
    name,
    state,
    version,
    spec,
    created_at,
    updated_at
FROM serialized
WHERE NOT EXISTS (
    SELECT 1
    FROM control_plane.resources AS active
    WHERE active.organization_id = serialized.organization_id
      AND active.project_id = serialized.project_id
      AND active.kind = 'TURN'
      AND active.spec ->> 'sessionId' = serialized.spec ->> 'sessionId'
      AND active.state IN (
          'CLAIMED',
          'RUNNING',
          'WAITING_OWNER',
          'WAITING_EXTERNAL',
          'BLOCKED'
      )
)
