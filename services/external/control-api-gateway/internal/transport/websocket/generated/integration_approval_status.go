
package generated

type IntegrationApprovalStatus uint

const (
  IntegrationApprovalStatusPending IntegrationApprovalStatus = iota
  IntegrationApprovalStatusApproved
  IntegrationApprovalStatusRejected
  IntegrationApprovalStatusExpired
  IntegrationApprovalStatusCancelled
)

// Value returns the value of the enum.
func (op IntegrationApprovalStatus) Value() any {
	if op >= IntegrationApprovalStatus(len(IntegrationApprovalStatusValues)) {
		return nil
	}
	return IntegrationApprovalStatusValues[op]
}

var IntegrationApprovalStatusValues = []any{"PENDING","APPROVED","REJECTED","EXPIRED","CANCELLED"}
var ValuesToIntegrationApprovalStatus = map[any]IntegrationApprovalStatus{
  IntegrationApprovalStatusValues[IntegrationApprovalStatusPending]: IntegrationApprovalStatusPending,
  IntegrationApprovalStatusValues[IntegrationApprovalStatusApproved]: IntegrationApprovalStatusApproved,
  IntegrationApprovalStatusValues[IntegrationApprovalStatusRejected]: IntegrationApprovalStatusRejected,
  IntegrationApprovalStatusValues[IntegrationApprovalStatusExpired]: IntegrationApprovalStatusExpired,
  IntegrationApprovalStatusValues[IntegrationApprovalStatusCancelled]: IntegrationApprovalStatusCancelled,
}
