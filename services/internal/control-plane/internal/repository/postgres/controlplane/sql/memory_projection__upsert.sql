-- name: MemoryProjectionUpsert
INSERT INTO control_plane.memory_vector_projections (
    resource_id,
    organization_id,
    project_id,
    resource_version,
    content_sha256,
    model_id,
    model_revision,
    model_sha256,
    embedding,
    projection_sha256,
    updated_at
) VALUES (
    @resource_id::uuid,
    @organization_id::uuid,
    @project_id::uuid,
    @resource_version,
    @content_sha256,
    @model_id,
    @model_revision,
    @model_sha256,
    @embedding::public.vector,
    @projection_sha256,
    @updated_at
)
ON CONFLICT (resource_id) DO UPDATE
SET
    organization_id = excluded.organization_id,
    project_id = excluded.project_id,
    resource_version = excluded.resource_version,
    content_sha256 = excluded.content_sha256,
    model_id = excluded.model_id,
    model_revision = excluded.model_revision,
    model_sha256 = excluded.model_sha256,
    embedding = excluded.embedding,
    projection_sha256 = excluded.projection_sha256,
    updated_at = excluded.updated_at
WHERE memory_vector_projections.resource_version <= excluded.resource_version
