-- name: platform_membership__insert :one
WITH target_subject AS (
    SELECT subject.id,
           subject.ref,
           subject.display_name,
           subject.email_masked,
           subject.active
    FROM control_plane.subjects subject
    WHERE subject.organization_id = @organization_id::uuid
      AND subject.ref = @user_ref
      AND subject.active
      AND subject.issuer = 'verified-oidc-subject'
      AND NOT EXISTS (
          SELECT 1
          FROM control_plane.memberships membership
          WHERE membership.organization_id = subject.organization_id
            AND membership.subject_id = subject.id
            AND membership.project_id IS NULL
      )
), inserted AS (
    INSERT INTO control_plane.memberships
        (ref, organization_id, project_id, subject_id, role, permissions, active)
    SELECT @membership_ref,
           @organization_id::uuid,
           NULL,
           target_subject.id,
           @platform_role,
           '{}'::text[],
           true
    FROM target_subject
    RETURNING ref, subject_id, role, active, version
)
SELECT inserted.ref,
       inserted.subject_id::text,
       target_subject.ref,
       target_subject.display_name,
       target_subject.email_masked,
       target_subject.active,
       inserted.role,
       inserted.active,
       inserted.version
FROM inserted
JOIN target_subject ON target_subject.id = inserted.subject_id;
