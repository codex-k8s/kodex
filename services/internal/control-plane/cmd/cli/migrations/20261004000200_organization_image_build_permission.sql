-- +goose Up
SET ROLE control_plane_owner;
-- +goose StatementBegin
DO $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM control_plane.permission_registry
   WHERE permission_key='image.build' AND resource_kinds=ARRAY['PROJECT','ROLE_IMAGE']::text[]) THEN
   RAISE EXCEPTION 'image.build resource registry precondition mismatch';
 END IF;
 UPDATE control_plane.permission_registry SET resource_kinds=ARRAY['ORGANIZATION','PROJECT','ROLE_IMAGE']::text[]
 WHERE permission_key='image.build';
END $$;
-- +goose StatementEnd
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'organization image permission migration is forward-only'; END $$;
-- +goose StatementEnd
