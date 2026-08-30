-- name: attachmentsets_insert_set :one
INSERT INTO control_plane.attachment_sets(
    ref, family_ref, revision, version, previous_revision_id, organization_id,
    project_id, state, source, purpose, manifest, manifest_digest,
    item_count, total_size_bytes, created_by, finalized_at
) VALUES (
    @attachment_set_ref, @family_ref, @revision, @revision,
    @previous_revision_id::uuid, @organization_id::uuid, NULLIF(@project_id, '')::uuid,
    @state, @source, @purpose, @manifest::jsonb, @manifest_digest,
    @item_count, @total_size_bytes, @created_by::uuid,
    CASE WHEN @state = 'FINALIZED' THEN clock_timestamp() ELSE NULL END
)
RETURNING id::text;
