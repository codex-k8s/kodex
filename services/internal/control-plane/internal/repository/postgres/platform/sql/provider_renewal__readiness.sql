-- name: provider_renewal__readiness :one
SELECT
    (SELECT array_agg(attribute.attname || ':' || format_type(attribute.atttypid, attribute.atttypmod) || ':' || attribute.attnotnull::text ORDER BY attribute.attnum)
       FROM pg_attribute attribute WHERE attribute.attrelid = 'control_plane.provider_credential_renewal_tasks'::regclass AND attribute.attnum > 0 AND NOT attribute.attisdropped)
    = ARRAY['id:uuid:true', 'ref:text:true', 'organization_id:uuid:true', 'provider_account_id:uuid:true', 'account_version:bigint:true',
      'input_credential_id:uuid:true', 'attempt:bigint:true', 'state:text:true', 'claimant_id:text:true', 'generation:bigint:true', 'fence:text:true',
      'request_digest:text:true', 'owner_request_digest:text:true', 'expires_at:timestamp with time zone:true', 'authorized_at:timestamp with time zone:false',
      'completed_at:timestamp with time zone:false', 'output_credential_id:uuid:false', 'output_account_version:bigint:false', 'materializer_receipt_uid:uuid:false',
      'materializer_receipt_version:text:false', 'result_digest:text:false', 'created_at:timestamp with time zone:true']
    AND EXISTS (SELECT 1 FROM pg_index idx WHERE idx.indexrelid = 'control_plane.provider_renewal_one_active'::regclass
        AND idx.indisvalid AND idx.indisunique AND idx.indnkeyatts = 1 AND idx.indnatts = 1 AND idx.indkey[0] = 4
        AND pg_get_expr(idx.indpred, idx.indrelid) = '(state = ANY (ARRAY[''CLAIMED''::text, ''AUTHORIZED''::text]))')
    AND EXISTS (SELECT 1 FROM pg_index idx WHERE idx.indexrelid = 'control_plane.provider_renewal_one_possible_effect'::regclass
        AND idx.indisvalid AND idx.indisunique AND idx.indnkeyatts = 1 AND idx.indnatts = 1 AND idx.indkey[0] = 6
        AND pg_get_expr(idx.indpred, idx.indrelid) = '(authorized_at IS NOT NULL)')
    AND EXISTS (SELECT 1 FROM pg_trigger trigger WHERE trigger.tgrelid = 'control_plane.provider_credential_renewal_tasks'::regclass
        AND trigger.tgname = 'protect_provider_renewal_task' AND trigger.tgenabled = 'O'
        AND trigger.tgfoid = 'control_plane.protect_provider_renewal_task()'::regprocedure)
    AND EXISTS (SELECT 1 FROM pg_trigger trigger WHERE trigger.tgrelid = 'control_plane.provider_accounts'::regclass
        AND trigger.tgname = 'cancel_stale_provider_renewal' AND trigger.tgenabled = 'O'
        AND trigger.tgfoid = 'control_plane.cancel_stale_provider_renewal()'::regprocedure);
