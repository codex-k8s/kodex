-- name: artifact_retention_grant_postcondition :one
SELECT EXISTS (SELECT 1 FROM control_plane.trusted_workload_generations
    WHERE workload_id = 'artifact-retention' AND enabled
      AND credential_generation BETWEEN 1 AND 9007199254740991)
    AND EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'control_plane.trusted_workload_generations'::regclass
        AND conname = 'trusted_workload_generations_workload_id_check' AND contype = 'c' AND convalidated AND conenforced
        AND pg_get_constraintdef(oid) = 'CHECK ((workload_id = ANY (ARRAY[''automation-scheduler''::text, ''control-plane''::text, ''email-bridge''::text, ''image-admission''::text, ''image-admission-controller''::text, ''image-promotion''::text, ''integration-gateway''::text, ''interaction-gateway''::text, ''role-image-builder''::text, ''runtime-controller''::text, ''secret-broker''::text, ''session-archive''::text, ''stt-tts-service''::text, ''artifact-retention''::text])))')
    AND EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'control_plane.worker_grant_high_watermarks'::regclass
        AND conname = 'worker_grant_high_watermarks_workload_id_check' AND contype = 'c' AND convalidated AND conenforced
        AND pg_get_constraintdef(oid) = 'CHECK ((workload_id = ANY (ARRAY[''automation-scheduler''::text, ''control-plane''::text, ''email-bridge''::text, ''image-admission''::text, ''image-promotion''::text, ''integration-gateway''::text, ''interaction-gateway''::text, ''role-image-builder''::text, ''runtime-controller''::text, ''secret-broker''::text, ''session-archive''::text, ''artifact-retention''::text])))');
