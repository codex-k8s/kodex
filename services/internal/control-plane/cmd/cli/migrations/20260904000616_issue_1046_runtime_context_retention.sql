-- +goose Up
SET ROLE control_plane_owner;
ALTER TABLE control_plane.runtime_revisions DROP CONSTRAINT runtime_revisions_safe_snapshot_check;
ALTER TABLE control_plane.runtime_revisions ADD CONSTRAINT runtime_revisions_safe_snapshot_check
    CHECK (octet_length(safe_snapshot::text)<=2097152);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.skill_artifact_reference_count(tenant uuid,artifact_ref text,artifact_revision bigint,artifact_digest text)
RETURNS bigint LANGUAGE sql STABLE SECURITY INVOKER AS $$
    SELECT (
        SELECT count(*) FROM control_plane.skill_bundle_revisions revision
        JOIN control_plane.skill_bundles bundle ON bundle.id=revision.bundle_id AND bundle.organization_id=revision.organization_id
        WHERE revision.organization_id=tenant AND bundle.state<>'PURGED' AND revision.state<>'DISCARDED'
          AND revision.files @> jsonb_build_array(jsonb_build_object(
              'ArtifactRef',artifact_ref,'ArtifactRevision',artifact_revision,'Digest',artifact_digest))
    ) + (
        SELECT count(*) FROM control_plane.runtime_revisions revision
        WHERE revision.organization_id=tenant
          AND EXISTS (SELECT 1 FROM control_plane.runtime_leases lease
                      WHERE lease.runtime_revision_id=revision.id AND lease.state='CLAIMED' AND lease.expires_at>statement_timestamp())
          AND EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(revision.safe_snapshot #> '{contextSnapshot,skills}','[]'::jsonb)) skill(item)
                      WHERE skill.item->'files' @> jsonb_build_array(jsonb_build_object(
                          'artifact_ref',artifact_ref,'artifact_revision',artifact_revision,'digest',artifact_digest)))
    );
$$;
-- +goose StatementEnd
RESET ROLE;
