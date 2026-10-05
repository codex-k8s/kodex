-- scenario: setup
CREATE SCHEMA control_plane;
CREATE TABLE control_plane.image_builds (stage text NOT NULL);
CREATE TABLE control_plane.runs (state text NOT NULL);
CREATE TABLE control_plane.runtime_leases (state text NOT NULL);
CREATE TABLE control_plane.image_artifacts (
    id uuid PRIMARY KEY, organization_id uuid NOT NULL, project_id uuid,
    admission_state text NOT NULL, promotion_state text NOT NULL,
    promotion_request_id uuid, manifest_digest text NOT NULL DEFAULT '',
    promoted_reference text NOT NULL DEFAULT '', policy_sha256 text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1
);
CREATE TABLE control_plane.role_image_promotion_requests (
    id uuid PRIMARY KEY, organization_id uuid NOT NULL, project_id uuid,
    image_artifact_id uuid NOT NULL REFERENCES control_plane.image_artifacts(id), state text NOT NULL
);
INSERT INTO control_plane.image_artifacts (id, organization_id, admission_state, promotion_state,
    manifest_digest, promoted_reference, policy_sha256)
SELECT md5(series::text)::uuid, '00000000-0000-4000-8000-000000000001', 'ACCEPTED', 'PROMOTED',
    'sha256:' || repeat('a', 64), 'registry.invalid/exact@sha256:' || repeat('a', 64), repeat('b', 64)
FROM generate_series(1, 19) series;
-- scenario: reset
DELETE FROM control_plane.role_image_promotion_requests;
DELETE FROM control_plane.image_artifacts WHERE promotion_state <> 'PROMOTED';
DELETE FROM control_plane.image_builds;
DELETE FROM control_plane.runs;
DELETE FROM control_plane.runtime_leases;
INSERT INTO control_plane.image_artifacts (id, organization_id, admission_state, promotion_state)
VALUES ('00000000-0000-4000-8000-000000000100', '00000000-0000-4000-8000-000000000001', 'ACCEPTED', 'PENDING');
-- scenario: unrequested
SELECT 1;
-- scenario: queued
INSERT INTO control_plane.role_image_promotion_requests (id, organization_id, image_artifact_id, state)
VALUES ('00000000-0000-4000-8000-000000000200', '00000000-0000-4000-8000-000000000001',
    '00000000-0000-4000-8000-000000000100', 'QUEUED');
UPDATE control_plane.image_artifacts SET promotion_request_id = '00000000-0000-4000-8000-000000000200'
WHERE id = '00000000-0000-4000-8000-000000000100';
-- scenario: claimed
INSERT INTO control_plane.role_image_promotion_requests (id, organization_id, image_artifact_id, state)
VALUES ('00000000-0000-4000-8000-000000000200', '00000000-0000-4000-8000-000000000001',
    '00000000-0000-4000-8000-000000000100', 'PROMOTING');
UPDATE control_plane.image_artifacts SET promotion_state = 'CLAIMED',
    promotion_request_id = '00000000-0000-4000-8000-000000000200'
WHERE id = '00000000-0000-4000-8000-000000000100';
-- scenario: authorized
INSERT INTO control_plane.role_image_promotion_requests (id, organization_id, image_artifact_id, state)
VALUES ('00000000-0000-4000-8000-000000000200', '00000000-0000-4000-8000-000000000001',
    '00000000-0000-4000-8000-000000000100', 'PROMOTING');
UPDATE control_plane.image_artifacts SET promotion_state = 'AUTHORIZED',
    promotion_request_id = '00000000-0000-4000-8000-000000000200'
WHERE id = '00000000-0000-4000-8000-000000000100';
-- scenario: orphan-claimed
UPDATE control_plane.image_artifacts SET promotion_state = 'CLAIMED'
WHERE id = '00000000-0000-4000-8000-000000000100';
-- scenario: orphan-authorized
UPDATE control_plane.image_artifacts SET promotion_state = 'AUTHORIZED'
WHERE id = '00000000-0000-4000-8000-000000000100';
-- scenario: foreign-project
INSERT INTO control_plane.role_image_promotion_requests (id, organization_id, project_id, image_artifact_id, state)
VALUES ('00000000-0000-4000-8000-000000000200', '00000000-0000-4000-8000-000000000001',
    '00000000-0000-4000-8000-000000000009', '00000000-0000-4000-8000-000000000100', 'PROMOTING');
UPDATE control_plane.image_artifacts SET promotion_state = 'CLAIMED',
    promotion_request_id = '00000000-0000-4000-8000-000000000200'
WHERE id = '00000000-0000-4000-8000-000000000100';
-- scenario: terminal-request-orphan
INSERT INTO control_plane.role_image_promotion_requests (id, organization_id, image_artifact_id, state)
VALUES ('00000000-0000-4000-8000-000000000200', '00000000-0000-4000-8000-000000000001',
    '00000000-0000-4000-8000-000000000100', 'PROMOTED');
UPDATE control_plane.image_artifacts SET promotion_state = 'AUTHORIZED',
    promotion_request_id = '00000000-0000-4000-8000-000000000200'
WHERE id = '00000000-0000-4000-8000-000000000100';
-- scenario: active-run
INSERT INTO control_plane.runs (state) VALUES ('RUNNING');
-- scenario: active-build
INSERT INTO control_plane.image_builds (stage) VALUES ('QUEUED');
-- scenario: admission-claim
UPDATE control_plane.image_artifacts SET admission_state = 'CLAIMED'
WHERE id = '00000000-0000-4000-8000-000000000100';
-- scenario: runtime-claim
INSERT INTO control_plane.runtime_leases (state) VALUES ('CLAIMED');
