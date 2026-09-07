-- name: proof_worker_grant_accept_instance :one
INSERT INTO control_plane.worker_grant_instance_high_watermarks AS current
    (workload_id, instance_id, credential_generation, revision, issued_at, expires_at, envelope_sha256)
SELECT $1, $2, $3, $4, $5::timestamptz, $6::timestamptz, $7
WHERE $6::timestamptz > clock_timestamp()
ON CONFLICT (workload_id, instance_id) DO UPDATE
SET credential_generation = EXCLUDED.credential_generation,
    revision = EXCLUDED.revision,
    issued_at = EXCLUDED.issued_at,
    expires_at = EXCLUDED.expires_at,
    envelope_sha256 = EXCLUDED.envelope_sha256,
    updated_at = clock_timestamp()
WHERE current.credential_generation < EXCLUDED.credential_generation
   OR (current.credential_generation = EXCLUDED.credential_generation
       AND current.revision < EXCLUDED.revision)
   OR (current.credential_generation = EXCLUDED.credential_generation
       AND current.revision = EXCLUDED.revision
       AND current.issued_at = EXCLUDED.issued_at
       AND current.expires_at = EXCLUDED.expires_at
       AND current.envelope_sha256 = EXCLUDED.envelope_sha256)
RETURNING revision;
