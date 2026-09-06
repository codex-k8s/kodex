-- name: prompt_scope_get :one
SELECT scope.target_kind,scope.target_ref,scope.template_kind,scope.context_pin
FROM control_plane.prompt_template_scopes scope
JOIN control_plane.managed_configuration_revisions revision ON revision.id=scope.revision_id
WHERE scope.organization_id=@organization_id::uuid AND revision.organization_id=scope.organization_id
  AND revision.ref=@revision_ref;
