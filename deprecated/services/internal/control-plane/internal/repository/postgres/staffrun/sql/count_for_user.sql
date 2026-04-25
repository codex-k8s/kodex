-- name: staffrun__count_for_user :one
SELECT COUNT(*)
FROM agent_runs ar
JOIN project_members pm ON pm.project_id = ar.project_id
WHERE pm.user_id = $1::uuid
  AND ar.project_id IS NOT NULL
  AND (
        COALESCE(ar.run_payload->'trigger'->>'label', '') ILIKE 'run:%'
        OR COALESCE(ar.run_payload->'trigger'->>'label', '') = 'mode:discussion'
        OR COALESCE(ar.run_payload->'trigger'->>'label', '') ILIKE 'need:reviewer'
      );
