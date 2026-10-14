-- name: provider_renewal__create_task :exec
INSERT INTO control_plane.provider_credential_renewal_tasks
    (ref, organization_id, provider_account_id, account_version, input_credential_id, attempt, state,
     claimant_id, generation, fence, request_digest, owner_request_digest, expires_at)
VALUES (@ref, @organization_id::uuid, @account_id::uuid, @account_version, @credential_id::uuid, @attempt, 'CLAIMED',
    @claimant, @generation, @fence, @request_digest, @owner_request_digest, @expires_at);
