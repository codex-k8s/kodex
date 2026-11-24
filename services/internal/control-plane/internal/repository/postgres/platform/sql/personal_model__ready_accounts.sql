-- name: personal_model__ready_accounts :many
SELECT account.ref
FROM control_plane.provider_accounts account
JOIN control_plane.subjects owner ON owner.id=account.personal_owner_subject_id
  AND owner.organization_id=account.organization_id AND owner.active AND owner.kind='USER'
WHERE account.organization_id=$1::uuid AND owner.id=$2::uuid AND account.definition_key=$3
  AND account.enabled AND account.state='AUTHORIZED' AND account.current_credential_revision_id IS NOT NULL
ORDER BY account.ref;
