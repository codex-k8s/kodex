-- name: RuntimeRestoreEffectAuthorize :one
WITH locked AS (
    SELECT operation.materialization_effect_generation,
           operation.materialization_effect_sha256,
           operation.credential_effect_generation,
           operation.credential_effect_sha256
    FROM control_plane.runtime_restore_operations AS operation
    WHERE operation.id = @id::uuid
    FOR UPDATE
), updated AS (
    UPDATE control_plane.runtime_restore_operations AS operation
    SET materialization_effect_generation = CASE
            WHEN @effect = 'KUBERNETES_MATERIALIZATION' THEN @generation
            ELSE operation.materialization_effect_generation
        END,
        materialization_effect_sha256 = CASE
            WHEN @effect = 'KUBERNETES_MATERIALIZATION' THEN @effect_sha256
            ELSE operation.materialization_effect_sha256
        END,
        credential_effect_generation = CASE
            WHEN @effect = 'S3_CREDENTIAL' THEN @generation
            ELSE operation.credential_effect_generation
        END,
        credential_effect_sha256 = CASE
            WHEN @effect = 'S3_CREDENTIAL' THEN @effect_sha256
            ELSE operation.credential_effect_sha256
        END,
        updated_at = CASE
            WHEN (@effect = 'KUBERNETES_MATERIALIZATION'
                  AND locked.materialization_effect_generation < @generation)
              OR (@effect = 'S3_CREDENTIAL'
                  AND locked.credential_effect_generation < @generation)
            THEN @updated_at
            ELSE operation.updated_at
        END
    FROM locked
    WHERE operation.id = @id::uuid
      AND operation.target_execution_id = @target_execution_id::uuid
      AND operation.generation = @generation
      AND operation.consumed_generation = @generation
      AND operation.revoked_generation < @generation
      AND operation.source_authority_sha256 = @source_authority_sha256
      AND (
          (@effect = 'KUBERNETES_MATERIALIZATION' AND (
              locked.materialization_effect_generation < @generation
              OR (locked.materialization_effect_generation = @generation
                  AND locked.materialization_effect_sha256 = @effect_sha256)
          ))
          OR (@effect = 'S3_CREDENTIAL' AND (
              locked.credential_effect_generation < @generation
              OR (locked.credential_effect_generation = @generation
                  AND locked.credential_effect_sha256 = @effect_sha256)
          ))
      )
    RETURNING CASE
        WHEN @effect = 'KUBERNETES_MATERIALIZATION'
            THEN locked.materialization_effect_generation < @generation
        ELSE locked.credential_effect_generation < @generation
    END AS applied
)
SELECT applied FROM updated;
