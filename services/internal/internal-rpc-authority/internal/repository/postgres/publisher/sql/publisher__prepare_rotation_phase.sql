-- name: publisher__prepare_rotation_phase :one
SELECT internal_rpc_authority.publisher_prepare_rotation_phase(
    $1::uuid, $2::text, $3::uuid, $4::bigint, $5::text,
    $6::bigint, $7::text, $8::integer, $9::text
);
