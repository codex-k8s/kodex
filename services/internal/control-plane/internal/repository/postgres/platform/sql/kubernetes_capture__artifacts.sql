-- name: kubernetes_capture__artifacts :many
SELECT saved.channel,saved.artifact_ref,saved.revision_ref,saved.digest,saved.size_bytes,
 capture.capture_generation,(saved.origin_json::jsonb->>'kind'='PARTIAL')
FROM control_plane.kubernetes_capture_artifact_revisions saved
JOIN control_plane.kubernetes_output_captures capture ON capture.id=saved.capture_id AND capture.organization_id=saved.organization_id
WHERE saved.capture_id=@capture_id::uuid AND saved.organization_id=@organization_id::uuid AND capture.state='COMPLETE'
ORDER BY saved.channel;
