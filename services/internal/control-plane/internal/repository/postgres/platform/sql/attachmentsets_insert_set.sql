-- name: attachmentsets_insert_set :one
INSERT INTO control_plane.attachment_sets(
    ref, organization_id, project_id, context_kind, manifest, manifest_digest,
    item_count, total_size_bytes, created_by
) VALUES (
    @attachment_set_ref, @organization_id::uuid, NULLIF(@project_id, '')::uuid, @context_kind, @manifest,
    @manifest_digest, @item_count, @total_size_bytes, @created_by::uuid
)
RETURNING id::text;
