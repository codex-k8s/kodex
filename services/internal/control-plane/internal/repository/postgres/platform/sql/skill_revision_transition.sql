-- name: skill_revision_transition :exec
UPDATE control_plane.skill_bundle_revisions SET state=@target_state,
    reviewed_by=CASE WHEN @review::boolean THEN @actor_id::uuid ELSE reviewed_by END,
    reviewed_at=CASE WHEN @review::boolean THEN clock_timestamp() ELSE reviewed_at END,
    review_comment=CASE WHEN @review::boolean THEN @comment ELSE review_comment END
WHERE organization_id=@organization_id::uuid AND bundle_id=@bundle_id::uuid AND ref=@revision_ref
    AND state=@source_state AND digest=@digest;
