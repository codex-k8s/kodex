-- name: kubernetes_manual_purge_terminal :one
SELECT (head.lifecycle_state='PURGED' AND head.retention_claim_generation>0
 AND head.retention_claim_owner IS NULL AND head.retention_claim_expires_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM control_plane.artifact_revisions revision WHERE revision.artifact_id=head.id)) AS terminal,
 (EXISTS(SELECT 1 FROM control_plane.prepared_content content
   WHERE content.organization_id=head.organization_id AND content.prepared_artifact_ref=head.ref AND content.state='ADOPTED')
  AND NOT EXISTS(SELECT 1 FROM control_plane.prepared_content content
   WHERE content.organization_id=head.organization_id AND content.prepared_artifact_ref=head.ref AND content.state='ADOPTED'
    AND(content.adopted_revision_id IS NOT NULL OR content.deletion_receipt_digest IS NULL
     OR content.object_key IS NOT NULL OR content.object_version IS NOT NULL OR content.digest IS NOT NULL))) AS scrubbed
FROM control_plane.artifact_heads head WHERE head.id=$1::uuid;
