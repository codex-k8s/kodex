-- +goose Up
SET ROLE control_plane_owner;
CREATE INDEX skill_revision_file_references ON control_plane.skill_bundle_revisions USING gin(files jsonb_path_ops);
-- +goose StatementBegin
CREATE FUNCTION control_plane.skill_artifact_reference_count(tenant uuid,artifact_ref text,artifact_revision bigint,artifact_digest text)
RETURNS bigint LANGUAGE sql STABLE SECURITY INVOKER AS $$
    SELECT count(*) FROM control_plane.skill_bundle_revisions revision
    JOIN control_plane.skill_bundles bundle ON bundle.id=revision.bundle_id AND bundle.organization_id=revision.organization_id
    WHERE revision.organization_id=tenant AND bundle.state<>'PURGED' AND revision.state<>'DISCARDED'
      AND revision.files @> jsonb_build_array(jsonb_build_object(
          'ArtifactRef',artifact_ref,'ArtifactRevision',artifact_revision,'Digest',artifact_digest));
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.skill_artifact_reference_count(uuid,text,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.skill_artifact_reference_count(uuid,text,bigint,text) TO control_plane_runtime;
-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_skill_artifact_retention()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='DELETE' OR NEW.lifecycle_state IS DISTINCT FROM OLD.lifecycle_state
       OR ROW(NEW.ref,NEW.revision,NEW.digest) IS DISTINCT FROM ROW(OLD.ref,OLD.revision,OLD.digest) THEN
        IF control_plane.skill_artifact_reference_count(OLD.organization_id,OLD.ref,OLD.revision,OLD.digest)>0 THEN
            RAISE EXCEPTION 'artifact is retained by skill revision';
        END IF;
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_skill_artifact_retention BEFORE UPDATE OR DELETE ON control_plane.artifacts
FOR EACH ROW EXECUTE FUNCTION control_plane.protect_skill_artifact_retention();
RESET ROLE;
