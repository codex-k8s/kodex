-- name: interaction_complete_delivery_resolve :one
SELECT
    d.id::text,
    d.project_id::text,
    project.ref,
    d.root_run_id::text,
    run.ref,
    COALESCE(d.gate_id::text, ''),
    d.capability_key,
    d.attempt,d.execution_max_attempts,
    d.created_at,
    d.external_team_ref,d.external_channel_ref,d.target_root_post_ref
FROM control_plane.interaction_deliveries d
JOIN control_plane.projects project ON project.id = d.project_id
JOIN control_plane.runs run ON run.id = d.root_run_id
WHERE d.organization_id = @organization_id::uuid
  AND d.ref = @delivery_ref
  AND d.state = 'CLAIMED'
  AND d.lease_ref = @lease_ref
  AND d.fence_digest = encode(digest(@fence, 'sha256'), 'hex')
  AND d.generation = @generation
  AND d.lease_expires_at > clock_timestamp()
FOR UPDATE
