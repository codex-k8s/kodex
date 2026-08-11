-- name: RuntimeStageInitial
SELECT integration_gateway.stage_initial_runtime_credential(
    @principal_name, @generation, @password, @not_before, @not_after
)
