-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.skill_bundle_revisions ADD CONSTRAINT skill_revision_parent_bundle_fk
    FOREIGN KEY(bundle_id,parent_revision_id) REFERENCES control_plane.skill_bundle_revisions(bundle_id,id);
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_skill_bundle()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' THEN RAISE EXCEPTION 'skill tombstone cannot be deleted'; END IF;
    IF ROW(NEW.id,NEW.ref,NEW.organization_id,NEW.project_id,NEW.created_by,NEW.created_at)
        IS DISTINCT FROM ROW(OLD.id,OLD.ref,OLD.organization_id,OLD.project_id,OLD.created_by,OLD.created_at) THEN
        RAISE EXCEPTION 'skill identity is immutable';
    END IF;
    IF OLD.state='PURGED' AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'purged skill is terminal'; END IF;
    IF NEW.state IS DISTINCT FROM OLD.state AND NOT (
        (OLD.state='ACTIVE' AND NEW.state='ARCHIVED') OR
        (OLD.state='ARCHIVED' AND NEW.state IN ('ACTIVE','PURGED'))) THEN
        RAISE EXCEPTION 'skill state transition is invalid';
    END IF;
    IF NEW.version<>OLD.version+1 AND NOT (OLD.current_revision_id IS NULL AND OLD.draft_revision_id IS NULL AND NEW.draft_revision_id IS NOT NULL AND OLD.version=1 AND NEW.version=1) THEN
        RAISE EXCEPTION 'skill version must advance';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_skill_bundle BEFORE UPDATE OR DELETE ON control_plane.skill_bundles
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_skill_bundle();
-- +goose StatementBegin
CREATE FUNCTION control_plane.skill_revision_projection(revision_id uuid)
RETURNS jsonb LANGUAGE sql STABLE SECURITY INVOKER AS $$
    SELECT jsonb_build_object('ref',revision.ref,'revision',revision.revision,'state',revision.state,
        'name',revision.name,'description',revision.description,'files',revision.files,'digest',revision.digest,
        'parentRevisionRef',COALESCE(parent.ref,''),'scanState',revision.scan_state,
        'scanEngine',revision.scan_engine,'scanDigest',revision.scan_digest,'scannedAt',revision.scanned_at,
        'reviewedBy',COALESCE(reviewer.ref,''),'reviewedAt',revision.reviewed_at,'diagnostics',revision.diagnostics,
        'provenance',jsonb_build_object('actorRef',actor.ref,'sourceKind','UI_UPLOAD','sourceRef',bundle.ref,
            'sourceRevision',revision.revision::text,'digest',revision.digest,'createdAt',revision.created_at))
    FROM control_plane.skill_bundle_revisions revision
    JOIN control_plane.skill_bundles bundle ON bundle.id=revision.bundle_id
    JOIN control_plane.subjects actor ON actor.id=revision.created_by
    LEFT JOIN control_plane.subjects reviewer ON reviewer.id=revision.reviewed_by
    LEFT JOIN control_plane.skill_bundle_revisions parent ON parent.id=revision.parent_revision_id
    WHERE revision.id=revision_id;
$$;
-- +goose StatementEnd
CREATE VIEW control_plane.skill_bundle_projection WITH (security_invoker=true) AS
SELECT bundle.id,bundle.organization_id,bundle.project_id,bundle.ref,bundle.version,bundle.state,
    bundle.current_revision_id,bundle.draft_revision_id,project.ref AS project_ref,
    COALESCE(draft.name,current.name,'') AS name,
    jsonb_build_object('ref',bundle.ref,'version',bundle.version,'projectRef',project.ref,'state',bundle.state,
        'currentRevision',control_plane.skill_revision_projection(bundle.current_revision_id),
        'draftRevision',control_plane.skill_revision_projection(bundle.draft_revision_id),
        'createdAt',bundle.created_at,'updatedAt',bundle.updated_at) AS projection
FROM control_plane.skill_bundles bundle
JOIN control_plane.projects project ON project.id=bundle.project_id AND project.organization_id=bundle.organization_id
LEFT JOIN control_plane.skill_bundle_revisions current ON current.id=bundle.current_revision_id
LEFT JOIN control_plane.skill_bundle_revisions draft ON draft.id=bundle.draft_revision_id;
REVOKE ALL ON FUNCTION control_plane.skill_revision_projection(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.skill_revision_projection(uuid) TO control_plane_runtime;
GRANT SELECT ON control_plane.skill_bundle_projection TO control_plane_runtime;
-- +goose StatementBegin
CREATE FUNCTION control_plane.skill_revision_visible(tenant uuid,actor uuid,revision_id uuid,evaluated_at timestamptz)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER AS $$
    SELECT revision_id IS NULL OR EXISTS (
        SELECT 1 FROM control_plane.skill_bundle_revisions revision
        WHERE revision.organization_id=tenant AND revision.id=revision_id
          AND NOT EXISTS (
            SELECT 1 FROM jsonb_array_elements(revision.files) file
            WHERE NOT EXISTS (
                SELECT 1 FROM control_plane.catalog_access_targets target
                WHERE target.organization_id=tenant AND target.kind='ARTIFACT' AND target.ref=file->>'ArtifactRef'
                  AND control_plane.catalog_resource_visible(tenant,actor,'artifact.view',target.kind,target.id,target.project_id,target.owner_id,target.related_ids,evaluated_at,false)
            )
          )
    );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.skill_revision_visible(uuid,uuid,uuid,timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.skill_revision_visible(uuid,uuid,uuid,timestamptz) TO control_plane_runtime;
RESET ROLE;
