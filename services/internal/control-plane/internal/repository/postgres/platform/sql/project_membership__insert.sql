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
), created AS (
    SELECT *
    FROM control_plane.create_project_membership(
        @membership_ref,
        @organization_id::uuid,
        @project_id::uuid,
        (SELECT id FROM target_subject),
        @actor_id::uuid,
        @permissions::text[]
    )
)
SELECT created.binding_ref,
       target_subject.ref,
       target_subject.display_name,
       target_subject.email_masked,
       target_subject.active,
       target_subject.platform_role,
       @permissions::text[],
       created.binding_state = 'ACTIVE',
       created.binding_version
FROM created CROSS JOIN target_subject;
