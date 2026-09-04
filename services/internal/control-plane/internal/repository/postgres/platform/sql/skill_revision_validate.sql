-- name: skill_revision_validate :exec
UPDATE control_plane.skill_bundle_revisions SET state=@state,scan_state=@scan_state,scan_engine=@scan_engine,
    scan_digest=@scan_digest,scanned_at=clock_timestamp(),reviewed_by=NULL,reviewed_at=NULL,review_comment='',diagnostics=@diagnostics::jsonb
WHERE organization_id=@organization_id::uuid AND bundle_id=@bundle_id::uuid AND ref=@revision_ref
    AND digest=@digest AND state IN ('DRAFT','INVALID','VALIDATED','REJECTED');
