-- +goose Up
SET ROLE control_plane_owner;

-- Сначала прекращается выдача proofs прежними CP issuers. Блокировка и trigger
-- дополнительно закрывают mixed-version race: legacy allocator не продолжает
-- независимо от нового watermark после commit этой миграции.
LOCK TABLE control_plane.installation IN SHARE ROW EXCLUSIVE MODE;

CREATE TABLE control_plane.authority_proof_revision_counter (
    singleton boolean PRIMARY KEY CHECK (singleton),
    revision bigint NOT NULL CHECK (revision >= 0 AND revision <= 9007199254740991)
);

-- +goose StatementBegin
DO $seed_counter$
BEGIN
    INSERT INTO control_plane.authority_proof_revision_counter (singleton, revision)
    SELECT singleton, authority_proof_sequence
    FROM control_plane.installation
    WHERE singleton;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'authority proof counter predecessor is missing';
    END IF;
END;
$seed_counter$;
-- +goose StatementEnd

REVOKE ALL ON control_plane.authority_proof_revision_counter FROM PUBLIC;
REVOKE ALL ON control_plane.authority_proof_revision_counter FROM control_plane_runtime;
GRANT SELECT, UPDATE ON control_plane.authority_proof_revision_counter
    TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.reject_legacy_authority_proof_counter()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog
AS $function$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '55000',
        MESSAGE = 'legacy authority proof allocator is disabled';
END;
$function$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION control_plane.reject_legacy_authority_proof_counter() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.reject_legacy_authority_proof_counter()
    TO control_plane_runtime;

CREATE TRIGGER reject_legacy_authority_proof_counter
BEFORE UPDATE OF authority_proof_sequence ON control_plane.installation
FOR EACH ROW EXECUTE FUNCTION control_plane.reject_legacy_authority_proof_counter();

RESET ROLE;

-- +goose Down
-- Старый image/allocator не являются rollback после выдачи новых revisions.
-- +goose StatementBegin
DO $forward_only$
BEGIN
    RAISE EXCEPTION 'authority proof counter migration requires forward recovery';
END;
$forward_only$;
-- +goose StatementEnd
