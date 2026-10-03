-- name: role_images_expire_builds :many
WITH expired AS (
    SELECT build.id FROM control_plane.image_builds build
    WHERE build.organization_id=$1::uuid
      AND build.stage IN ('MATERIALIZATION','CONTEXT_VALIDATION','BASE_PULL','SOLVING','INSTALLATION','TRUSTED_RUNTIME_FINALIZATION','STAGING_PUSH','PROVENANCE')
      AND build.lease_expires_at<=clock_timestamp()
    ORDER BY build.lease_expires_at,build.ref
    LIMIT 32 FOR UPDATE SKIP LOCKED
)
UPDATE control_plane.image_builds build
SET stage=CASE WHEN build.attempt>=build.maximum_attempts THEN 'DEAD_LETTER' ELSE 'EXPIRED' END,
    safe_error_code='BUILD_LEASE_EXPIRED',claimant_workload=NULL,authority_generation=0,
    lease_token_sha256=NULL,lease_expires_at=NULL,fence=build.fence+1,
    available_at=clock_timestamp(),version=build.version+1,updated_at=clock_timestamp()
FROM expired WHERE build.id=expired.id
RETURNING build.ref,(SELECT recipe.ref FROM control_plane.role_image_recipes recipe WHERE recipe.id=build.recipe_id),
          build.version,build.stage,COALESCE(build.project_id::text,''),build.scope_kind;
