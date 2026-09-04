-- name: proof_worker_grant_accept_high_watermark :one
WITH advanced AS (
    INSERT INTO control_plane.worker_grant_high_watermarks
        (workload_id, credential_generation, revision, issued_at, expires_at)
    VALUES ($1, $2, $3, $4, $5)
    ON CONFLICT (workload_id) DO UPDATE
    SET credential_generation = EXCLUDED.credential_generation,
        revision = EXCLUDED.revision,
        issued_at = EXCLUDED.issued_at,
        expires_at = EXCLUDED.expires_at,
        updated_at = clock_timestamp()
    WHERE control_plane.worker_grant_high_watermarks.credential_generation < EXCLUDED.credential_generation
       OR (control_plane.worker_grant_high_watermarks.credential_generation = EXCLUDED.credential_generation
           AND control_plane.worker_grant_high_watermarks.revision < EXCLUDED.revision)
    RETURNING revision
)
SELECT revision FROM advanced
UNION ALL
SELECT revision
FROM control_plane.worker_grant_high_watermarks
WHERE workload_id = $1
  AND credential_generation = $2
  AND revision = $3
  AND issued_at = $4
  AND expires_at = $5
LIMIT 1
