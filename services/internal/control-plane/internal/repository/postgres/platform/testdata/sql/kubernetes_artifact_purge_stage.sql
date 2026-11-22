-- name: kubernetes_artifact_purge_stage :one
SELECT bool_and(head.lifecycle_state='PURGED' AND head.purged_at IS NOT NULL AND saved.revision_id IS NULL)
FROM control_plane.kubernetes_capture_artifact_revisions saved
JOIN control_plane.artifact_heads head ON head.id=saved.artifact_id AND head.organization_id=saved.organization_id
JOIN control_plane.kubernetes_output_stages stage ON stage.stream_id=saved.stream_id AND stage.organization_id=saved.organization_id AND stage.channel=saved.channel
WHERE saved.artifact_ref=$1 AND stage.payload_purged_at IS NULL;
