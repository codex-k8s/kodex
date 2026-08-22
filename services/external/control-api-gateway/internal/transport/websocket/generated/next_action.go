
package generated

type NextAction uint

const (
  NextActionOpen NextAction = iota
  NextActionEdit
  NextActionArchive
  NextActionEnable
  NextActionDisable
  NextActionValidate
  NextActionPublish
  NextActionRollback
  NextActionLaunch
  NextActionAddTurn
  NextActionCancel
  NextActionRetry
  NextActionResolveGate
  NextActionDownload
  NextActionBind
  NextActionTest
  NextActionRevoke
  NextActionApplyPlan
  NextActionRecover
)

// Value returns the value of the enum.
func (op NextAction) Value() any {
	if op >= NextAction(len(NextActionValues)) {
		return nil
	}
	return NextActionValues[op]
}

var NextActionValues = []any{"OPEN","EDIT","ARCHIVE","ENABLE","DISABLE","VALIDATE","PUBLISH","ROLLBACK","LAUNCH","ADD_TURN","CANCEL","RETRY","RESOLVE_GATE","DOWNLOAD","BIND","TEST","REVOKE","APPLY_PLAN","RECOVER"}
var ValuesToNextAction = map[any]NextAction{
  NextActionValues[NextActionOpen]: NextActionOpen,
  NextActionValues[NextActionEdit]: NextActionEdit,
  NextActionValues[NextActionArchive]: NextActionArchive,
  NextActionValues[NextActionEnable]: NextActionEnable,
  NextActionValues[NextActionDisable]: NextActionDisable,
  NextActionValues[NextActionValidate]: NextActionValidate,
  NextActionValues[NextActionPublish]: NextActionPublish,
  NextActionValues[NextActionRollback]: NextActionRollback,
  NextActionValues[NextActionLaunch]: NextActionLaunch,
  NextActionValues[NextActionAddTurn]: NextActionAddTurn,
  NextActionValues[NextActionCancel]: NextActionCancel,
  NextActionValues[NextActionRetry]: NextActionRetry,
  NextActionValues[NextActionResolveGate]: NextActionResolveGate,
  NextActionValues[NextActionDownload]: NextActionDownload,
  NextActionValues[NextActionBind]: NextActionBind,
  NextActionValues[NextActionTest]: NextActionTest,
  NextActionValues[NextActionRevoke]: NextActionRevoke,
  NextActionValues[NextActionApplyPlan]: NextActionApplyPlan,
  NextActionValues[NextActionRecover]: NextActionRecover,
}
