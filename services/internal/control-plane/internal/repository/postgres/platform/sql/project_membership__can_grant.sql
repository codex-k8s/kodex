-- name: project_membership__can_grant :one
SELECT @actor_platform_role IN ('OWNER', 'ADMINISTRATOR')
    OR EXISTS (
        SELECT 1
        FROM control_plane.memberships actor_membership
        WHERE actor_membership.organization_id = @organization_id::uuid
          AND actor_membership.project_id = @project_id::uuid
          AND actor_membership.subject_id = @actor_id::uuid
          AND actor_membership.active
          AND NOT EXISTS (
              SELECT requested.permission
              FROM unnest(@requested_permissions::text[]) AS requested(permission)
              WHERE NOT (requested.permission = ANY(actor_membership.permissions))
          )
    );
