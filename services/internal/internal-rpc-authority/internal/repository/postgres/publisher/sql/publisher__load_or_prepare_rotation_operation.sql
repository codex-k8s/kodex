-- name: publisher__load_or_prepare_rotation_operation :one
SELECT operation_id, registry_revision, registry_digest_sha256,
       base_revision, base_digest_sha256, expected_readback_count, status,
       switch_not_before, previous_not_after, completed_at
FROM internal_rpc_authority.publisher_load_or_prepare_rotation_operation(
    $1::uuid, $2::bigint, $3::text, $4::bigint, $5::text, $6::integer
);
