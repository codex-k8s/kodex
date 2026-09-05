package platform

import _ "embed"

var (
	//go:embed sql/environment_draft_get.sql
	queryEnvironmentDraftGet string
	//go:embed sql/environment_draft_lock.sql
	queryEnvironmentDraftLock string
	//go:embed sql/environment_draft_insert.sql
	queryEnvironmentDraftInsert string
	//go:embed sql/environment_draft_update.sql
	queryEnvironmentDraftUpdate string
)
