-- name: repository_bundle__candidates :many
SELECT publication.ref
FROM control_plane.repository_bundle_publications publication
WHERE publication.organization_id=@organization_id::uuid
  AND publication.state IN ('WAITING_APPROVAL','QUEUED','CLAIMED','EFFECT_STARTED','UNKNOWN_OUTCOME')
  AND (publication.lease_expires_at IS NULL OR publication.lease_expires_at<=clock_timestamp())
  AND (publication.state<>'WAITING_APPROVAL' OR publication.expires_at<=clock_timestamp())
  AND (publication.state<>'UNKNOWN_OUTCOME' OR publication.updated_at<=clock_timestamp()-interval '30 seconds')
ORDER BY publication.updated_at,publication.id
FOR UPDATE OF publication SKIP LOCKED
LIMIT 1;
