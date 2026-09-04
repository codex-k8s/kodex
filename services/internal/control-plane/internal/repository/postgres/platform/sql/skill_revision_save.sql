-- name: skill_revision_save :exec
UPDATE control_plane.skill_bundle_revisions SET name=@name,description=@description,files=@files::jsonb,digest=@digest,
    state='DRAFT',scan_state='PENDING',scan_engine='',scan_digest='',scanned_at=NULL,
    reviewed_by=NULL,reviewed_at=NULL,review_comment='',diagnostics='[]'::jsonb
WHERE organization_id=@organization_id::uuid AND bundle_id=@bundle_id::uuid AND ref=@revision_ref
    AND state NOT IN ('PUBLISHED','DISCARDED');
