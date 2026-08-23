-- name: project_membership__insert :one
WITH target_subject AS (
    SELECT subject.id,
           subject.ref,
           subject.display_name,
           subject.email_masked,
           subject.active,
           platform_membership.role AS platform_role
    FROM control_plane.subjects subject
    JOIN control_plane.memberships platform_membership
      ON platform_membership.organization_id = subject.organization_id
     AND platform_membership.subject_id = subject.id
     AND platform_membership.project_id IS NULL
     AND platform_membership.active
    WHERE subject.organization_id = @organization_id::uuid
      AND subject.ref = @user_ref
      AND subject.active
      AND subject.issuer = 'verified-oidc-subject'
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.memberships project_membership
          WHERE project_membership.organization_id = subject.organization_id
            AND project_membership.project_id = @project_id::uuid
            AND project_membership.subject_id = subject.id
      )
), inserted AS (
    INSERT INTO control_plane.memberships
        (ref, organization_id, project_id, subject_id, role, permissions, active)
    SELECT @membership_ref,
           @organization_id::uuid,
           @project_id::uuid,
           target_subject.id,
           'MEMBER',
           @permissions::text[],
           true
    FROM target_subject
    RETURNING ref, subject_id, permissions, active, version
)
SELECT inserted.ref,
       target_subject.ref,
       target_subject.display_name,
       target_subject.email_masked,
       target_subject.active,
       target_subject.platform_role,
       inserted.permissions,
       inserted.active,
       inserted.version
FROM inserted
JOIN target_subject ON target_subject.id = inserted.subject_id;
