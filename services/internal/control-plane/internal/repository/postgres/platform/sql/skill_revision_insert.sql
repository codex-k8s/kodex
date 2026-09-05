-- name: skill_revision_insert :one
INSERT INTO control_plane.skill_bundle_revisions
(ref,organization_id,bundle_id,revision,state,name,description,files,digest,parent_revision_id,created_by)
SELECT @revision_ref,@organization_id::uuid,bundle.id,
    COALESCE((SELECT max(revision) FROM control_plane.skill_bundle_revisions WHERE bundle_id=bundle.id),0)+1,
    'DRAFT',@name,@description,@files::jsonb,@digest,bundle.current_revision_id,@actor_id::uuid
FROM control_plane.skill_bundles bundle WHERE bundle.organization_id=@organization_id::uuid AND bundle.id=@bundle_id::uuid
    AND bundle.state='ACTIVE' AND bundle.draft_revision_id IS NULL
RETURNING id::text;
