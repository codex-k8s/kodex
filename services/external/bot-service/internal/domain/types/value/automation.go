package value

type AutomationSchedulePreset string

const AutomationSchedulePresetDaily AutomationSchedulePreset = "daily"

type AutomationRunSource string

const (
	AutomationRunSourceManual    AutomationRunSource = "manual"
	AutomationRunSourceScheduled AutomationRunSource = "scheduled"
)

type AutomationRunStatus string

const (
	AutomationRunStatusQueued       AutomationRunStatus = "queued"
	AutomationRunStatusRunning      AutomationRunStatus = "running"
	AutomationRunStatusWaitingOwner AutomationRunStatus = "waiting_owner"
	AutomationRunStatusSucceeded    AutomationRunStatus = "succeeded"
	AutomationRunStatusFailed       AutomationRunStatus = "failed"
)

type AutomationRunOutcome string

const (
	AutomationRunOutcomeNoAction      AutomationRunOutcome = "no_action"
	AutomationRunOutcomeActionTaken   AutomationRunOutcome = "action_taken"
	AutomationRunOutcomeRequiresHuman AutomationRunOutcome = "requires_human"
	AutomationRunOutcomeFailed        AutomationRunOutcome = "failed"
)

const (
	AutomationPlaybookProjectCheckV1 = "project_check_v1"
	AutomationPromptVersionV1        = "automation.project_check.v1"
	AutomationCallbackContractV1     = "automation.callback.v1"
)
