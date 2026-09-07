-- name: proof_worker_grant_accept_generation :one
INSERT INTO control_plane.worker_grant_high_watermarks AS current
    (workload_id, credential_generation, revision, issued_at, expires_at)
SELECT $1, $2, 1, $3::timestamptz, $4::timestamptz
WHERE $3::timestamptz <= clock_timestamp() + interval '5 seconds'
  AND $4::timestamptz > clock_timestamp()
  AND $4::timestamptz <= $3::timestamptz + interval '4 minutes'
ON CONFLICT (workload_id) DO UPDATE
SET credential_generation = EXCLUDED.credential_generation,
    revision = CASE WHEN current.credential_generation < EXCLUDED.credential_generation
        THEN 1 ELSE current.revision END,
    issued_at = CASE WHEN current.credential_generation < EXCLUDED.credential_generation
        THEN EXCLUDED.issued_at ELSE current.issued_at END,
    expires_at = CASE WHEN current.credential_generation < EXCLUDED.credential_generation
        THEN EXCLUDED.expires_at ELSE current.expires_at END,
    updated_at = clock_timestamp()
WHERE current.credential_generation <= EXCLUDED.credential_generation
RETURNING credential_generation;
