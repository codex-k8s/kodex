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
          SELECT 1 FROM control_plane.access_bindings binding
          WHERE binding.organization_id = subject.organization_id
            AND binding.subject_id = subject.id
            AND binding.presentation_kind = 'PLATFORM_MEMBERSHIP'
      )
), target_role AS (
    SELECT role_version.id, role.stable_key
    FROM control_plane.application_roles role
    JOIN control_plane.application_role_versions role_version ON role_version.id = role.current_version_id
    WHERE role.organization_id = @organization_id::uuid
      AND role.kind = 'SYSTEM'
      AND role.stable_key = @platform_role
      AND role.state = 'ACTIVE'
), inserted AS (
    INSERT INTO control_plane.access_bindings
        (ref, organization_id, subject_kind, subject_id, role_version_id, scope_kind,
         presentation_kind, state, created_by)
    SELECT @membership_ref,
           @organization_id::uuid,
           'USER',
           target_subject.id,
           target_role.id,
           'ORGANIZATION',
           'PLATFORM_MEMBERSHIP',
           'ACTIVE',
           @actor_id::uuid
    FROM target_subject CROSS JOIN target_role
    RETURNING ref, subject_id, role_version_id, state, version
)
SELECT inserted.ref,
       inserted.subject_id::text,
       target_subject.ref,
       target_subject.display_name,
       target_subject.email_masked,
       target_subject.active,
       target_role.stable_key,
       inserted.state = 'ACTIVE',
       inserted.version
FROM inserted
JOIN target_subject ON target_subject.id = inserted.subject_id
JOIN target_role ON target_role.id = inserted.role_version_id;
