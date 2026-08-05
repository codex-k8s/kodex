-- +goose Up
ALTER TABLE control_plane.resources
    DROP CONSTRAINT resources_kind_check,
    ADD CONSTRAINT resources_kind_check CHECK (kind IN (
        'PROJECT', 'TEAM', 'CHAT', 'ROLE', 'PROMPT_PROFILE',
        'CREDENTIAL_BINDING', 'REPOSITORY_WORKSPACE', 'INTEGRATION',
        'RUNTIME_REVISION', 'SESSION', 'TURN', 'PROCESS_RUN', 'SCHEDULE',
        'OWNER_GATE', 'MEMORY_RECORD', 'WORK_CLAIM', 'ARTIFACT',
        'ROLE_IMAGE_RECIPE', 'IMAGE_BUILD', 'IMAGE_ARTIFACT'
    )),
    ADD CONSTRAINT role_image_sensitive_fields_scope CHECK (
        kind = 'ROLE_IMAGE_RECIPE'
        OR (
            NOT (spec ? 'installationBlock')
            AND NOT (spec ? 'buildSecretRefs')
            AND NOT (spec ? 'input')
        )
    ),
    ADD CONSTRAINT role_image_recipe_shape CHECK (
        kind <> 'ROLE_IMAGE_RECIPE'
        OR (
            jsonb_typeof(spec -> 'input') = 'object'
            AND jsonb_typeof(spec #> '{input,installationBlock}') = 'string'
            AND coalesce(spec ->> 'specSha256', '') ~ '^[a-f0-9]{64}$'
            AND coalesce(spec ->> 'policySha256', '') ~ '^[a-f0-9]{64}$'
            AND coalesce(spec ->> 'generation', '0')::numeric >= 1
            AND coalesce(spec ->> 'policyRevision', '0')::numeric >= 1
        )
    ),
    ADD CONSTRAINT role_image_build_shape CHECK (
        kind <> 'IMAGE_BUILD'
        OR (
            coalesce(spec ->> 'specSha256', '') ~ '^[a-f0-9]{64}$'
            AND coalesce(spec ->> 'immutableBuildSha256', '') ~ '^[a-f0-9]{64}$'
            AND coalesce(spec ->> 'attempt', '0')::integer BETWEEN 1 AND 10
        )
    ),
    ADD CONSTRAINT role_image_artifact_shape CHECK (
        kind <> 'IMAGE_ARTIFACT'
        OR (
            coalesce(spec ->> 'specSha256', '') ~ '^[a-f0-9]{64}$'
            AND coalesce(spec ->> 'manifestDigest', '') ~ '^sha256:[a-f0-9]{64}$'
            AND coalesce(spec ->> 'immutableBuildSha256', '') ~ '^[a-f0-9]{64}$'
            AND coalesce(spec ->> 'stagingReference', '') LIKE '%@' || (spec ->> 'manifestDigest')
            AND (
                coalesce(spec ->> 'admissionVerdict', '') = ''
                OR (
                    spec ->> 'admissionVerdict' IN ('ACCEPTED', 'REJECTED')
                    AND coalesce(spec ->> 'admissionReceiptSha256', '') ~ '^[a-f0-9]{64}$'
                    AND coalesce(spec ->> 'admissionReceiptOciManifestDigest', '') ~ '^sha256:[a-f0-9]{64}$'
                )
            )
            AND (
                state <> 'ACTIVE'
                OR (
                    spec ->> 'admissionVerdict' = 'ACCEPTED'
                    AND coalesce(spec ->> 'admissionReceiptSha256', '') ~ '^[a-f0-9]{64}$'
                    AND coalesce(spec ->> 'admissionReceiptOciManifestDigest', '') ~ '^sha256:[a-f0-9]{64}$'
                    AND coalesce(spec ->> 'signatureSha256', '') ~ '^[a-f0-9]{64}$'
                    AND coalesce(spec ->> 'promotionReadbackSha256', '') ~ '^[a-f0-9]{64}$'
                    AND coalesce(spec ->> 'promotedReference', '') LIKE '%@' || (spec ->> 'manifestDigest')
                    AND coalesce(spec ->> 'promotionClaimJtiSha256', '') = ''
                )
            )
        )
    );

CREATE INDEX resources_image_build_queue_idx
    ON control_plane.resources (
        organization_id,
        project_id,
        ((spec ->> 'availableAt')::timestamptz),
        created_at,
        id
    )
    WHERE kind = 'IMAGE_BUILD' AND state = 'QUEUED';

CREATE INDEX resources_image_admission_queue_idx
    ON control_plane.resources (organization_id, project_id, created_at, id)
    WHERE kind = 'IMAGE_ARTIFACT' AND state = 'WAITING_EXTERNAL';

CREATE INDEX resources_promoted_image_spec_idx
    ON control_plane.resources (
        organization_id,
        project_id,
        (spec ->> 'specSha256'),
        ((spec ->> 'policyRevision')::bigint),
        (spec ->> 'policySha256')
    )
    WHERE kind = 'IMAGE_ARTIFACT' AND state = 'ACTIVE';

-- +goose Down
DO $forward_only$
BEGIN
    RAISE EXCEPTION
        'migration 20260805019500 is forward-only: role image authority state cannot be discarded'
        USING ERRCODE = '0A000';
END
$forward_only$;
