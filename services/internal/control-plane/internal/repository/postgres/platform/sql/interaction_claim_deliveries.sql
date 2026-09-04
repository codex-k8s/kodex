-- name: interaction_claim_deliveries :many
WITH expired AS (
    UPDATE control_plane.interaction_deliveries
    SET state = 'UNKNOWN_OUTCOME',
        lease_ref = NULL,
        fence_digest = NULL,
        workload_instance = NULL,
        lease_expires_at = NULL,
        available_at = clock_timestamp(),
        safe_error_code = 'INTERACTION_OUTCOME_UNKNOWN',
        version = version + 1,
        updated_at = clock_timestamp()
    WHERE organization_id = @organization_id::uuid
      AND state = 'CLAIMED'
      AND lease_expires_at <= clock_timestamp()
    RETURNING id
), candidates AS (
    SELECT d.id, gen_random_uuid()::text AS fence
    FROM control_plane.interaction_deliveries d
    JOIN control_plane.integration_connections c ON c.id = d.connection_id
    JOIN control_plane.integration_grants g ON g.id = d.grant_id
    LEFT JOIN control_plane.owner_gates gate ON gate.id = d.gate_id
    WHERE d.organization_id = @organization_id::uuid
      AND d.state IN ('DUE', 'FAILED')
      AND d.attempt < 10
      AND d.available_at <= clock_timestamp()
      AND c.enabled
      AND c.state IN ('CONNECTED', 'DEGRADED')
      AND g.enabled
      AND (d.gate_id IS NULL OR gate.state = 'OPEN')
      AND (SELECT count(*) FROM expired) >= 0
    ORDER BY d.available_at, d.created_at
    FOR UPDATE OF d SKIP LOCKED
    LIMIT @claim_limit
), claimed AS (
    UPDATE control_plane.interaction_deliveries d
    SET state = 'CLAIMED',
        attempt = d.attempt + 1,
        generation = d.generation + 1,
        lease_ref = 'idl_' || replace(gen_random_uuid()::text, '-', ''),
        fence_digest = encode(digest(candidate.fence, 'sha256'), 'hex'),
        workload_instance = @workload_instance,
        lease_expires_at = clock_timestamp() + interval '45 seconds',
        safe_error_code = '',
        version = d.version + 1,
        updated_at = clock_timestamp()
    FROM candidates candidate
    WHERE d.id = candidate.id
    RETURNING d.*, candidate.fence
)
SELECT
    claimed.ref,
    c.ref,
    COALESCE(c.credential_materialization_ref, credential_revision.ref),
    c.public_configuration->>'base_url',
    c.public_configuration->>'team_name',
    c.public_configuration->>'channel_name',
    project.language,
    claimed.capability_key,
    claimed.message_key,
    claimed.template_data,
    claimed.lease_ref,
    claimed.fence,
    claimed.generation,
    claimed.lease_expires_at,
    COALESCE(gate.ref,''),COALESCE(gate.version,0),run.ref
FROM claimed
JOIN control_plane.integration_connections c ON c.id = claimed.connection_id
LEFT JOIN control_plane.integration_credential_revisions credential_revision
  ON credential_revision.id = c.credential_revision_id
JOIN control_plane.projects project ON project.id = claimed.project_id
JOIN control_plane.runs run ON run.id=claimed.root_run_id
LEFT JOIN control_plane.owner_gates gate ON gate.id=claimed.gate_id
ORDER BY claimed.created_at
