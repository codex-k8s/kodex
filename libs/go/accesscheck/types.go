package accesscheck

import (
	"errors"
	"time"
)

const (
	// DefaultCallerType is used by internal platform services.
	DefaultCallerType = "service"
	// DefaultTimeout bounds one access-manager decision request.
	DefaultTimeout = 3 * time.Second
)

var (
	// ErrInvalidRequest means the request cannot be sent to access-manager.
	ErrInvalidRequest = errors.New("invalid access check request")
	// ErrForbidden means access-manager denied the operation.
	ErrForbidden = errors.New("access denied")
	// ErrDependencyUnavailable means access-manager did not return a usable decision.
	ErrDependencyUnavailable = errors.New("access-manager unavailable")
)

// Config contains the shared access-manager client settings.
type Config struct {
	Addr       string
	AuthToken  string
	CallerType string
	CallerID   string
	Timeout    time.Duration
}

// Request describes one access-manager CheckAccess decision.
type Request struct {
	Subject        Subject
	ActionKey      string
	Resource       Resource
	Scope          Scope
	RequestID      string
	RequestContext RequestContext
}

// Subject identifies the actor whose access is checked.
type Subject struct {
	Type string
	ID   string
}

// Resource identifies the target resource.
type Resource struct {
	Type string
	ID   string
}

// Scope identifies the effective scope for the resource.
type Scope struct {
	Type string
	ID   string
}

// RequestContext carries safe audit metadata.
type RequestContext struct {
	Source       string
	TraceID      string
	SessionID    string
	ClientIPHash string
}

// RequestFields is a flat adapter input for service-specific authorization requests.
type RequestFields struct {
	SubjectType  string
	SubjectID    string
	ActionKey    string
	ResourceType string
	ResourceID   string
	ScopeType    string
	ScopeID      string
	RequestID    string
	Context      RequestContext
}

// DomainErrors maps shared accesscheck errors to service-domain errors.
type DomainErrors struct {
	InvalidRequest        error
	Forbidden             error
	DependencyUnavailable error
}
