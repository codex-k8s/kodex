-- name: staffrun__count_all :one
SELECT COUNT(*)
FROM agent_runs ar
WHERE (
        COALESCE(ar.run_payload->'trigger'->>'label', '') ILIKE 'run:%'
        OR COALESCE(ar.run_payload->'trigger'->>'label', '') = 'mode:discussion'
        OR COALESCE(ar.run_payload->'trigger'->>'label', '') ILIKE 'need:reviewer'
    );
