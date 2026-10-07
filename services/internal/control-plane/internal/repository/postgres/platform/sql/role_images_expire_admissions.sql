-- name: role_images_expire_admissions :many
WITH expired AS (
    SELECT artifact.id
    FROM control_plane.image_artifacts artifact
    WHERE artifact.organization_id = @organization_id::uuid
      AND artifact.admission_state = 'CLAIMED'
      AND artifact.admission_claim_expires_at <= clock_timestamp()
    ORDER BY artifact.admission_claim_expires_at, artifact.ref
    FOR UPDATE SKIP LOCKED LIMIT 32
), terminal AS (
    UPDATE control_plane.image_artifacts artifact
    SET admission_state = 'FAILED', admission_failure_code = 'ADMISSION_LEASE_EXPIRED',
        admission_failure_authority_generation = admission_authority_generation,
        admission_claimant_workload = NULL, admission_authority_generation = 0,
        admission_claim_token_sha256 = NULL, admission_claim_expires_at = NULL,
        version = version + 1, updated_at = clock_timestamp()
    FROM expired WHERE artifact.id = expired.id
    RETURNING artifact.*
)
SELECT terminal.ref, terminal.version, recipe.ref, terminal.recipe_generation,
       build.ref, terminal.build_attempt, terminal.scope_kind,
       (SELECT ref FROM control_plane.organizations WHERE id = terminal.organization_id),
       COALESCE(project.ref, ''), COALESCE(terminal.project_id::text, ''), terminal.recipe_version
FROM terminal
JOIN control_plane.role_image_recipes recipe ON recipe.id = terminal.recipe_id
JOIN control_plane.image_builds build ON build.id = terminal.build_id
LEFT JOIN control_plane.projects project ON project.id = terminal.project_id
