package restore

import _ "embed"

//go:embed sql/restore_fence__apply.sql
var applyFenceSQL string

//go:embed sql/restore_fence__readiness.sql
var fenceReadinessSQL string
