-- name: package_version__set_verification :exec
UPDATE package_hub_package_versions
SET verification_status = @verification_status,
    release_status = @release_status,
    revision = @revision,
    updated_at = @updated_at
WHERE id = @id
  AND package_id = @package_id
  AND revision = @previous_revision;
