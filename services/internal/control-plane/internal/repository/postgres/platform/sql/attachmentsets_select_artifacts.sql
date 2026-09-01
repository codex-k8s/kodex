-- name: attachmentsets_select_artifacts :many
WITH requested AS (
    SELECT ref, position
    FROM unnest(@artifact_refs::text[]) WITH ORDINALITY AS item(ref, position)
)
SELECT artifact.id::text,
       artifact.ref,
       artifact.revision,
       artifact.version,
       artifact.file_name,
       artifact.media_type,
       artifact.size_bytes,
       artifact.digest,
       artifact.source,
       requested.position
FROM requested
JOIN control_plane.artifacts AS artifact
  ON artifact.organization_id = @organization_id::uuid
 AND artifact.project_id IS NOT DISTINCT FROM NULLIF(@project_id, '')::uuid
 AND artifact.ref = requested.ref
 AND artifact.scan_state = 'CLEAN'
 AND artifact.lifecycle_state = 'ACTIVE'
ORDER BY requested.position;
