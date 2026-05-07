-- name: package_verification__create :exec
INSERT INTO package_hub_package_verifications (
    id,
    package_version_id,
    verification_status,
    verified_by_actor_ref,
    verification_notes,
    created_at
) VALUES (
    @id,
    @package_version_id,
    @verification_status,
    @verified_by_actor_ref,
    @verification_notes,
    @created_at
);
