package platform

import _ "embed"

var (
	//go:embed sql/workflow_launch__resume_step_run.sql
	queryWorkflowLaunchResumeStepRun string
	//go:embed sql/workflow_launch__run_pending.sql
	queryWorkflowLaunchRunPending string
	//go:embed sql/workflow_launch__claim_authorities.sql
	queryWorkflowLaunchClaimAuthorities string
	//go:embed sql/workflow_launch__terminal_roots.sql
	queryWorkflowLaunchTerminalRoots string
	//go:embed sql/workflow_launch__lock_projects.sql
	queryWorkflowLaunchLockProjects string
	//go:embed sql/workflow_launch__resolve_origin.sql
	queryWorkflowLaunchResolveOrigin string
	//go:embed sql/workflow_launch__existing.sql
	queryWorkflowLaunchExisting string
	//go:embed sql/workflow_launch__bound.sql
	queryWorkflowLaunchBound string
	//go:embed sql/workflow_launch__insert_proxy.sql
	queryWorkflowLaunchInsertProxy string
	//go:embed sql/workflow_launch__insert.sql
	queryWorkflowLaunchInsert string
	//go:embed sql/workflow_launch__bind_parent.sql
	queryWorkflowLaunchBindParent string
	//go:embed sql/workflow_launch__failed.sql
	queryWorkflowLaunchFailed string
	//go:embed sql/workflow_launch__terminal.sql
	queryWorkflowLaunchTerminal string
	//go:embed sql/workflow_launch__complete.sql
	queryWorkflowLaunchComplete string
	//go:embed sql/workflow_launch__claim_origin.sql
	queryWorkflowLaunchClaimOrigin string
)
