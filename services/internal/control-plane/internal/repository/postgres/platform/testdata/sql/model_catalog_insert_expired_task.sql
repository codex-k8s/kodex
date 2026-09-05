INSERT INTO control_plane.provider_model_catalog_tasks
  (ref, organization_id, provider_account_id, account_version, provider_credential_revision_id,
   authorization_method, state, claimant_id, claim_generation, fence, request_digest, expires_at)
SELECT $1, account.organization_id, account.id, account.version, credential.id,
       $2, 'CLAIMED', $3, $4, $5, $6, $7
FROM control_plane.provider_accounts account
JOIN control_plane.provider_credential_revisions credential ON credential.id = account.current_credential_revision_id
WHERE account.organization_id = $8::uuid AND account.ref = $9;
