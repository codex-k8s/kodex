-- name: model_catalog__resolve_proof :one
SELECT subject.id::text, organization.id::text, subject.updated_at, organization.version
FROM control_plane.provider_model_catalog_tasks task
JOIN control_plane.provider_accounts account ON account.id = task.provider_account_id AND account.organization_id = task.organization_id
JOIN control_plane.provider_credential_revisions credential ON credential.id = task.provider_credential_revision_id
  AND credential.provider_account_id = account.id AND credential.organization_id = task.organization_id
JOIN control_plane.organizations organization ON organization.id = task.organization_id
JOIN control_plane.subjects subject ON subject.organization_id = organization.id AND subject.issuer = 'kodex-system' AND subject.ref = 'sys_platform'
JOIN control_plane.provider_definitions definition ON definition.stable_key = account.definition_key
WHERE task.request_digest = $1 AND task.state = 'CLAIMED' AND task.expires_at > clock_timestamp()
  AND account.version = task.account_version AND account.current_credential_revision_id = task.provider_credential_revision_id
  AND account.enabled AND account.state = 'AUTHORIZED' AND definition.enabled AND subject.active
  AND EXISTS (SELECT 1 FROM control_plane.access_bindings binding
      JOIN control_plane.application_role_versions role_version ON role_version.id = binding.role_version_id
      JOIN control_plane.application_roles role ON role.id = role_version.role_id
      WHERE binding.organization_id = organization.id AND binding.subject_id = subject.id
        AND binding.scope_kind = 'ORGANIZATION' AND binding.state = 'ACTIVE'
        AND role.kind = 'SYSTEM' AND role.stable_key = 'OWNER');
