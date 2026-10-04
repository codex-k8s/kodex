-- +goose Up
SET ROLE control_plane_owner;

-- PostgreSQL сам канонизирует точное прежнее выражение. Временная проверка
-- лишь сужает boundary и удаляется в той же owner-транзакции после сравнения.
-- +goose StatementBegin
DO $$
DECLARE previous_definition text; expected_definition text;
BEGIN
 SELECT pg_get_constraintdef(oid) INTO previous_definition FROM pg_constraint
 WHERE conrelid='control_plane.access_bindings'::regclass AND conname='access_bindings_scope_shape' AND convalidated;
 IF previous_definition IS NULL OR EXISTS (SELECT 1 FROM pg_constraint
   WHERE conrelid='control_plane.access_bindings'::regclass AND conname='access_bindings_expected_previous_scope') THEN
  RAISE EXCEPTION 'access binding scope precondition is missing' USING ERRCODE='23514';
 END IF;
 ALTER TABLE control_plane.access_bindings ADD CONSTRAINT access_bindings_expected_previous_scope CHECK (
  (scope_kind='ORGANIZATION' AND project_id IS NULL AND resource_kind IS NULL AND resource_id IS NULL) OR
  (scope_kind='PROJECT' AND project_id IS NOT NULL AND resource_kind IS NULL AND resource_id IS NULL) OR
  (scope_kind='RESOURCE_KIND' AND resource_kind IS NOT NULL AND resource_id IS NULL) OR
  (scope_kind='RESOURCE_INSTANCE' AND resource_kind IS NOT NULL AND resource_id IS NOT NULL AND
   (project_id IS NOT NULL OR resource_kind IN ('INTEGRATION','ARTIFACT')))
 ) NOT VALID;
 SELECT replace(pg_get_constraintdef(oid),' NOT VALID','') INTO expected_definition FROM pg_constraint
 WHERE conrelid='control_plane.access_bindings'::regclass AND conname='access_bindings_expected_previous_scope';
 IF previous_definition IS DISTINCT FROM expected_definition THEN
  RAISE EXCEPTION 'access binding scope definition does not match previous revision' USING ERRCODE='23514';
 END IF;
 ALTER TABLE control_plane.access_bindings DROP CONSTRAINT access_bindings_expected_previous_scope;
 ALTER TABLE control_plane.access_bindings DROP CONSTRAINT access_bindings_scope_shape;
 ALTER TABLE control_plane.access_bindings ADD CONSTRAINT access_bindings_scope_shape CHECK (
  (scope_kind='ORGANIZATION' AND project_id IS NULL AND resource_kind IS NULL AND resource_id IS NULL) OR
  (scope_kind='PROJECT' AND project_id IS NOT NULL AND resource_kind IS NULL AND resource_id IS NULL) OR
  (scope_kind='RESOURCE_KIND' AND resource_kind IS NOT NULL AND resource_id IS NULL) OR
  (scope_kind='RESOURCE_INSTANCE' AND resource_kind IS NOT NULL AND resource_id IS NOT NULL AND
   (project_id IS NOT NULL OR resource_kind IN ('INTEGRATION','ARTIFACT','PROVIDER_ACCOUNT')))
 );
END $$;
-- +goose StatementEnd
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'provider account instance binding migration is forward-only'; END $$;
-- +goose StatementEnd
