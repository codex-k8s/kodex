-- name: publisher__advance_rotation_operation :one
SELECT operation_id, registry_revision, registry_digest_sha256,
       base_revision, base_digest_sha256, expected_readback_count, status,
       switch_not_before, previous_not_after, completed_at
FROM internal_rpc_authority.publisher_advance_rotation_operation(
    $1::uuid, $2::bigint, $3::text, $4::bigint, $5::text, $6::integer,
    $7::text, $8::uuid, $9::bigint, $10::text, $11::text
);
