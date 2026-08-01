-- name: MemorySearch
WITH ranked AS (
    SELECT
        resource.id,
        resource.organization_id,
        resource.project_id,
        resource.parent_id,
        resource.owner_actor_id,
        resource.kind,
        resource.name,
        resource.state,
        resource.version,
        resource.spec,
        resource.created_at,
        resource.updated_at,
        CASE
            WHEN @query = '' THEN 0::real
            ELSE ts_rank_cd(
                to_tsvector(
                    'simple',
                    coalesce(resource.spec ->> 'title', '') || ' ' ||
                    coalesce(resource.spec ->> 'content', '')
                ),
                websearch_to_tsquery('simple', @query),
                32
            )
        END AS text_rank,
        CASE
            WHEN @query_embedding = ''
              OR projection.resource_id IS NULL
              OR projection.resource_version <> resource.version
              OR projection.content_sha256 <> resource.spec ->> 'contentSha256'
              OR projection.model_id <> @model_id
              OR projection.model_revision <> @model_revision
              OR projection.model_sha256 <> @model_sha256
            THEN NULL
            ELSE projection.embedding <=> @query_embedding::public.vector
        END AS vector_distance
    FROM control_plane.resources AS resource
    LEFT JOIN control_plane.memory_vector_projections AS projection
      ON projection.resource_id = resource.id
     AND projection.organization_id = resource.organization_id
     AND projection.project_id = resource.project_id
    WHERE resource.organization_id = @organization_id::uuid
      AND resource.project_id = @project_id::uuid
      AND resource.kind = 'MEMORY_RECORD'
      AND resource.state <> 'DELETED'
      AND (cardinality(@states::text[]) = 0 OR resource.state = ANY(@states::text[]))
      AND (@parent_id = '' OR resource.parent_id = @parent_id::uuid)
      AND (
          (
              resource.spec ->> 'scope' = 'PROJECT'
              AND @can_read_project
          )
          OR (
              resource.spec ->> 'scope' = 'ROLE'
              AND resource.spec ->> 'roleId' = ANY(@actor_role_ids::text[])
          )
      )
      AND (@scope = '' OR resource.spec ->> 'scope' = @scope)
      AND (@role_id = '' OR resource.spec ->> 'roleId' = @role_id)
      AND (
          @query = ''
          OR to_tsvector(
              'simple',
              coalesce(resource.spec ->> 'title', '') || ' ' ||
              coalesce(resource.spec ->> 'content', '')
          ) @@ websearch_to_tsquery('simple', @query)
      )
)
SELECT
    id::text,
    organization_id::text,
    project_id::text,
    coalesce(parent_id::text, ''),
    owner_actor_id::text,
    kind,
    name,
    state,
    version,
    spec,
    created_at,
    updated_at,
    text_rank,
    coalesce(vector_distance, 0),
    vector_distance IS NOT NULL
FROM ranked
WHERE (
    @generic_order
    AND (@after_id = '' OR id > @after_id::uuid)
)
OR (
    NOT @generic_order
    AND (
    @after_id = ''
    OR (vector_distance IS NOT NULL)::integer < @after_vector_used::integer
    OR (
        (vector_distance IS NOT NULL)::integer = @after_vector_used::integer
        AND text_rank < @after_text_rank::real
    )
    OR (
        (vector_distance IS NOT NULL)::integer = @after_vector_used::integer
        AND text_rank = @after_text_rank::real
        AND coalesce(vector_distance, 0) > @after_vector_distance::real
    )
    OR (
        (vector_distance IS NOT NULL)::integer = @after_vector_used::integer
        AND text_rank = @after_text_rank::real
        AND coalesce(vector_distance, 0) = @after_vector_distance::real
        AND id > @after_id::uuid
    ))
)
ORDER BY
    CASE WHEN @generic_order THEN 0 ELSE (vector_distance IS NOT NULL)::integer END DESC,
    CASE WHEN @generic_order THEN 0 ELSE text_rank END DESC,
    CASE WHEN @generic_order THEN 0 ELSE vector_distance END ASC NULLS LAST,
    id
LIMIT @limit
