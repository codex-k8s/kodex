-- name: prompt_preview_snapshot :one
SELECT revision.safe_snapshot -> 'promptSnapshot'
FROM control_plane.runtime_revisions revision
JOIN control_plane.runs run ON run.id = revision.run_id
JOIN control_plane.sessions session ON session.id = revision.session_id
WHERE revision.organization_id = @organization_id::uuid
  AND revision.safe_snapshot ? 'promptSnapshot'
  AND (
      (@target_kind = 'RUN' AND run.ref = @target_ref)
      OR (@target_kind = 'SESSION' AND session.ref = @target_ref)
  )
  AND (
      @actor_platform_role IN ('OWNER', 'ADMINISTRATOR')
      OR EXISTS (
          SELECT 1
          FROM control_plane.memberships membership
          WHERE membership.organization_id = revision.organization_id
            AND membership.project_id = run.project_id
            AND membership.subject_id = @actor_id::uuid
            AND membership.active
            AND 'VIEW' = ANY(membership.permissions)
      )
  )
ORDER BY revision.created_at DESC, revision.ref DESC
LIMIT 1;
