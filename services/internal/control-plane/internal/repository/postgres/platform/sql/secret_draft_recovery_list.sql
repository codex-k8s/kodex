-- name: secret_draft_recovery_list :many
SELECT o.ref,d.ref FROM control_plane.runtime_secret_draft_operations o
JOIN control_plane.runtime_secret_drafts d ON d.id=o.draft_id
WHERE o.organization_id=@organization_id::uuid AND o.ref>@cursor_ref
AND (NOT o.cleanup_completed
OR (d.expires_at<=clock_timestamp() AND d.state IN ('PREPARING','DRAFT','VALID','PUBLISHING'))
OR (o.kind='PUBLISH' AND o.state='COMPLETED' AND d.state='PUBLISHED'
    AND EXISTS (SELECT 1 FROM control_plane.runtime_secret_revisions revision
                WHERE revision.secret_id=d.secret_id AND revision.revision=o.target_revision AND revision.state='ACTIVE')))
AND ((o.state='CLAIMED' AND o.lease_deadline<=clock_timestamp())
OR (o.state='PREPARED' AND o.grant_expires_at<=clock_timestamp())
OR o.state='FAILED' OR d.state IN ('PUBLISHED','DISCARDED','EXPIRED')
OR d.expires_at<=clock_timestamp())
ORDER BY o.ref LIMIT @page_size;
