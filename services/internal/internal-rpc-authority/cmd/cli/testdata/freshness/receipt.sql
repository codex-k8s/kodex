SELECT accepted_at, expires_at FROM internal_rpc_authority.authority_readback_attestation_receipts WHERE receipt_id = $1;
