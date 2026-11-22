-- name: kubernetes_capture__base :many
WITH prior AS MATERIALIZED(
 SELECT capture.* FROM control_plane.kubernetes_output_captures capture
 WHERE capture.organization_id=@organization_id::uuid AND capture.stream_id=@stream_id::uuid
  AND capture.capture_generation<@capture_generation AND capture.state='COMPLETE' AND capture.capture_kind='PARTIAL'
  AND capture.acknowledged_prefix<=@acknowledged_watermark
 ORDER BY capture.capture_generation DESC LIMIT 1)
SELECT prior.acknowledged_prefix,prior.source_snapshot,prior.snapshot_digest,prior.source_digest,
 (SELECT count(*) FROM control_plane.kubernetes_capture_artifact_revisions expected WHERE expected.capture_id=prior.id AND expected.organization_id=prior.organization_id),
 saved.channel,saved.origin_json,saved.digest,saved.size_bytes,
 coalesce(content.object_key,''),coalesce(content.object_version,''),coalesce(content.object_etag,'')
FROM prior JOIN control_plane.kubernetes_capture_artifact_revisions saved ON saved.capture_id=prior.id AND saved.organization_id=prior.organization_id
LEFT JOIN control_plane.artifact_revision_content content ON content.revision_id=saved.revision_id
ORDER BY saved.channel;
