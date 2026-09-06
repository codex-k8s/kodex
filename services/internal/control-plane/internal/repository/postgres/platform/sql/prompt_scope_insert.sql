-- name: prompt_scope_insert :exec
INSERT INTO control_plane.prompt_template_scopes(revision_id,organization_id,target_kind,target_ref,template_kind,context_pin)
SELECT revision.id, revision.organization_id, @target_kind, @target_ref, @template_kind, @context_pin::jsonb
FROM control_plane.managed_configuration_revisions revision
JOIN control_plane.managed_configuration_sets configuration ON configuration.id=revision.configuration_set_id
WHERE revision.organization_id=@organization_id::uuid AND revision.ref=@revision_ref
  AND configuration.kind='PROMPT_TEMPLATE' AND configuration.organization_id=revision.organization_id
  AND revision.state='DRAFT';
