package enum

type RiskLevel string

const (
	RiskRead                  RiskLevel = "READ"
	RiskWrite                 RiskLevel = "WRITE"
	RiskExternalCommunication RiskLevel = "EXTERNAL_COMMUNICATION"
	RiskDestructive           RiskLevel = "DESTRUCTIVE"
	RiskFinancial             RiskLevel = "FINANCIAL"
	RiskPlatformAdmin         RiskLevel = "PLATFORM_ADMIN"
)

func (value RiskLevel) Valid() bool {
	switch value {
	case RiskRead, RiskWrite, RiskExternalCommunication, RiskDestructive, RiskFinancial, RiskPlatformAdmin:
		return true
	default:
		return false
	}
}

func (value RiskLevel) RequiresApproval() bool {
	switch value {
	case RiskWrite, RiskExternalCommunication, RiskDestructive, RiskFinancial, RiskPlatformAdmin:
		return true
	default:
		return false
	}
}

type ApprovalPolicy string

const (
	ApprovalNever  ApprovalPolicy = "NEVER"
	ApprovalAlways ApprovalPolicy = "ALWAYS"
)

func (value ApprovalPolicy) ValidFor(risk RiskLevel) bool {
	if value != ApprovalNever && value != ApprovalAlways {
		return false
	}
	return !risk.RequiresApproval() || value == ApprovalAlways
}

type IdempotencyMode string

const (
	IdempotencyNone           IdempotencyMode = "NONE"
	IdempotencyProviderHeader IdempotencyMode = "PROVIDER_HEADER"
)

func (value IdempotencyMode) Valid() bool {
	return value == IdempotencyNone || value == IdempotencyProviderHeader
}

type ConnectionStatus string

const (
	ConnectionPending ConnectionStatus = "PENDING"
	ConnectionValid   ConnectionStatus = "VALID"
	ConnectionInvalid ConnectionStatus = "INVALID"
	ConnectionRevoked ConnectionStatus = "REVOKED"
)

type GrantStatus string

const (
	GrantActive  GrantStatus = "ACTIVE"
	GrantRevoked GrantStatus = "REVOKED"
	GrantExpired GrantStatus = "EXPIRED"
)

type SessionStatus string

const (
	SessionInitializing SessionStatus = "INITIALIZING"
	SessionActive       SessionStatus = "ACTIVE"
	SessionClosed       SessionStatus = "CLOSED"
	SessionExpired      SessionStatus = "EXPIRED"
)

type InvocationStatus string

const (
	InvocationPendingApproval InvocationStatus = "PENDING_APPROVAL"
	InvocationApproved        InvocationStatus = "APPROVED"
	InvocationRejected        InvocationStatus = "REJECTED"
	InvocationExecuting       InvocationStatus = "EXECUTING"
	InvocationSucceeded       InvocationStatus = "SUCCEEDED"
	InvocationFailed          InvocationStatus = "FAILED"
	InvocationUnknown         InvocationStatus = "UNKNOWN"
	InvocationCancelled       InvocationStatus = "CANCELLED"
	InvocationExpired         InvocationStatus = "EXPIRED"
)

func (value InvocationStatus) Terminal() bool {
	switch value {
	case InvocationRejected, InvocationSucceeded, InvocationFailed, InvocationUnknown, InvocationCancelled, InvocationExpired:
		return true
	default:
		return false
	}
}

type ApprovalStatus string

const (
	ApprovalPending   ApprovalStatus = "PENDING"
	ApprovalApproved  ApprovalStatus = "APPROVED"
	ApprovalRejected  ApprovalStatus = "REJECTED"
	ApprovalCancelled ApprovalStatus = "CANCELLED"
	ApprovalExpired   ApprovalStatus = "EXPIRED"
)

type ContinuationAction string

const (
	ContinuationNone    ContinuationAction = "NONE"
	ContinuationSuspend ContinuationAction = "SUSPEND"
	ContinuationApprove ContinuationAction = "APPROVE"
	ContinuationReject  ContinuationAction = "REJECT"
	ContinuationCancel  ContinuationAction = "CANCEL"
	ContinuationExpire  ContinuationAction = "EXPIRE"
	ContinuationBegin   ContinuationAction = "BEGIN"
	ContinuationSucceed ContinuationAction = "SUCCEED"
	ContinuationFail    ContinuationAction = "FAIL"
)

type ValidationCode string

const (
	ValidationOK                    ValidationCode = "OK"
	ValidationCredentialUnavailable ValidationCode = "CREDENTIAL_UNAVAILABLE"
	ValidationUnauthorized          ValidationCode = "UNAUTHORIZED"
	ValidationForbidden             ValidationCode = "FORBIDDEN"
	ValidationEndpointUnavailable   ValidationCode = "ENDPOINT_UNAVAILABLE"
	ValidationTimeout               ValidationCode = "TIMEOUT"
	ValidationProtocolError         ValidationCode = "PROTOCOL_ERROR"
)
