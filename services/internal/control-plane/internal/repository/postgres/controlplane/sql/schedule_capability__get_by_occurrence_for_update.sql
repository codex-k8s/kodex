SELECT id::text, organization_id::text, project_id::text, occurrence_id::text,
       attempt, immutable_input_sha256, authority_generation, full_method,
       workload_id, caller_spiffe_id, token_sha256, state, issued_at, expires_at,
       coalesce(consumed_at, 'epoch'::timestamptz), coalesce(revoked_at, 'epoch'::timestamptz)
FROM control_plane.schedule_occurrence_capabilities
WHERE occurrence_id = @occurrence_id::uuid
  AND attempt = @attempt
  AND full_method = @full_method
  AND authority_generation = @authority_generation
FOR UPDATE
