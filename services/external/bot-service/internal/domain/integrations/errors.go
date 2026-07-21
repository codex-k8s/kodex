package integrations

import "errors"

var (
	ErrUnauthorized         = errors.New("integration authorization denied")
	ErrInvalidInput         = errors.New("integration input is invalid")
	ErrIdempotencyConflict  = errors.New("integration idempotency binding conflict")
	ErrApprovalBinding      = errors.New("integration approval binding mismatch")
	ErrApprovalActor        = errors.New("integration approval actor denied")
	ErrApprovalTerminal     = errors.New("integration approval is terminal")
	ErrAuthorizationChanged = errors.New("integration execution authorization changed")
	ErrReceiptMissing       = errors.New("integration execution receipt is missing")
	ErrNoExecution          = errors.New("integration execution is not available")
)

// ReasonCode возвращает стабильный безопасный код без текста инфраструктурной ошибки.
func ReasonCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidInput):
		return "request.invalid"
	case errors.Is(err, ErrIdempotencyConflict):
		return "idempotency.arguments_conflict"
	case errors.Is(err, ErrApprovalBinding):
		return "approval.binding_mismatch"
	case errors.Is(err, ErrApprovalActor):
		return "approval.actor_denied"
	case errors.Is(err, ErrApprovalTerminal):
		return "approval.terminal"
	case errors.Is(err, ErrAuthorizationChanged):
		return "execution.authorization_changed"
	case errors.Is(err, ErrReceiptMissing):
		return "execution.receipt_missing"
	default:
		return "authorization.denied"
	}
}
