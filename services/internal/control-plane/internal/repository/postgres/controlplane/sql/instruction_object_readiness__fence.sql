-- name: InstructionObjectReadinessFence
SELECT pg_advisory_xact_lock(hashtextextended(
    'control-plane:instruction-object-store-readiness:v1',
    0
));
