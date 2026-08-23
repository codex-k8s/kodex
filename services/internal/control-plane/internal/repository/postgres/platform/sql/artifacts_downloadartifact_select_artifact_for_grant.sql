-- name: artifacts_downloadartifact_select_artifact_for_grant :one
SELECT ar.id,
       ar.project_id,
       ar.version,
       ar.scan_state
FROM control_plane.artifacts ar
WHERE ar.organization_id = @organization_id::uuid
  AND ar.ref = @artifact_ref
  AND (
      @platform_role IN ('OWNER', 'ADMINISTRATOR')
      OR EXISTS (
          SELECT 1
          FROM control_plane.memberships m
          WHERE m.organization_id = ar.organization_id
            AND m.project_id = ar.project_id
            AND m.subject_id = @subject_id::uuid
            AND m.active
            AND 'VIEW' = ANY(m.permissions)
      )
  )
FOR UPDATE OF ar;
