-- name: skill_bundle_transition :exec
UPDATE control_plane.skill_bundles SET state=@state,version=version+1,updated_at=clock_timestamp(),
    current_revision_id=CASE WHEN @publish::boolean THEN draft_revision_id ELSE current_revision_id END,
    draft_revision_id=CASE WHEN @clear_draft::boolean THEN NULL ELSE draft_revision_id END
WHERE organization_id=@organization_id::uuid AND id=@bundle_id::uuid AND version=@version;
