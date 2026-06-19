-- +goose Up
ALTER TABLE governance_manager_blocking_signals
    DROP CONSTRAINT IF EXISTS governance_manager_blocking_signals_source_type_chk;

ALTER TABLE governance_manager_blocking_signals
    ADD CONSTRAINT governance_manager_blocking_signals_source_type_chk
        CHECK (source_type IN (
            'acceptance',
            'review_signal',
            'runtime',
            'provider',
            'interaction',
            'human',
            'monitoring',
            'security',
            'dependency',
            'container',
            'infrastructure'
        ));

-- +goose Down
ALTER TABLE governance_manager_blocking_signals
    DROP CONSTRAINT IF EXISTS governance_manager_blocking_signals_source_type_chk;

ALTER TABLE governance_manager_blocking_signals
    ADD CONSTRAINT governance_manager_blocking_signals_source_type_chk
        CHECK (source_type IN (
            'acceptance',
            'review_signal',
            'runtime',
            'provider',
            'interaction',
            'human',
            'monitoring'
        ));
