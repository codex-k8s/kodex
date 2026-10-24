UPDATE control_plane.access_bindings binding SET role_version_id = $4::uuid
FROM control_plane.projects project
WHERE binding.organization_id = $1::uuid AND binding.subject_id = $2::uuid
  AND project.id = binding.project_id AND project.ref = $3
  AND binding.presentation_kind = 'PROJECT_MEMBERSHIP' AND binding.state = 'ACTIVE';
