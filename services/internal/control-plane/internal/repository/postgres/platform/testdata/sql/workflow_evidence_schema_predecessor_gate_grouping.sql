-- Fault injection: SUPERSEDED grouping нельзя ослаблять общей OR-веткой.
ALTER TABLE control_plane.owner_gates DROP CONSTRAINT owner_gate_closure_kind;
ALTER TABLE control_plane.owner_gates ADD CONSTRAINT owner_gate_closure_kind CHECK (closure_kind='' OR closure_kind='SUPERSEDED' AND state='CANCELLED' OR decision IS NULL);
