-- name: GetLegacyProvenanceProjection :one
SELECT coalesce(to_jsonb(provenance)::text, '')
FROM control_plane.legacy_graph_provenance AS provenance
WHERE provenance.plan_id = @plan_id::uuid
  AND provenance.ordinal = @ordinal
