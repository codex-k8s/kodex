-- +goose Up
ALTER TABLE access_secret_binding_refs
    DROP CONSTRAINT IF EXISTS access_secret_binding_refs_store_type_check;

UPDATE access_secret_binding_refs
SET store_type = 'kubernetes_mounted_secret',
    updated_at = now(),
    version = version + 1
WHERE store_type = 'kubernetes_secret';

ALTER TABLE access_secret_binding_refs
    ADD CONSTRAINT access_secret_binding_refs_store_type_check
    CHECK (store_type IN ('vault', 'kubernetes_mounted_secret', 'env'));

-- +goose Down
ALTER TABLE access_secret_binding_refs
    DROP CONSTRAINT IF EXISTS access_secret_binding_refs_store_type_check;

UPDATE access_secret_binding_refs
SET store_type = 'kubernetes_secret',
    updated_at = now(),
    version = version + 1
WHERE store_type = 'kubernetes_mounted_secret';

ALTER TABLE access_secret_binding_refs
    ADD CONSTRAINT access_secret_binding_refs_store_type_check
    CHECK (store_type IN ('vault', 'kubernetes_secret'));
