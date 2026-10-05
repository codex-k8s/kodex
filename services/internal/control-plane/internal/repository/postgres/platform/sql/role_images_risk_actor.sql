-- name: role_images_risk_actor :one
SELECT role.stable_key
FROM control_plane.subjects subject
JOIN control_plane.access_bindings binding ON binding.subject_id=subject.id
 AND binding.organization_id=subject.organization_id AND binding.subject_kind='USER'
JOIN control_plane.application_role_versions version ON version.id=binding.role_version_id
JOIN control_plane.application_roles role ON role.id=version.role_id
WHERE subject.organization_id=@organization_id::uuid AND subject.id=@actor_id::uuid
 AND subject.kind='USER' AND subject.active AND binding.state='ACTIVE'
 AND binding.scope_kind='ORGANIZATION' AND role.kind='SYSTEM'
 AND role.stable_key IN ('OWNER','ADMINISTRATOR')
 AND (binding.valid_from IS NULL OR binding.valid_from<=clock_timestamp())
 AND (binding.valid_until IS NULL OR binding.valid_until>clock_timestamp())
ORDER BY role.stable_key LIMIT 1
FOR SHARE OF subject,binding
