// Package enum задаёт закрытые состояния runtime-controller.
package enum

type ExecutionState string

const (
	ExecutionPending   ExecutionState = "PENDING"
	ExecutionAdmitted  ExecutionState = "ADMITTED"
	ExecutionRunning   ExecutionState = "RUNNING"
	ExecutionSucceeded ExecutionState = "SUCCEEDED"
	ExecutionFailed    ExecutionState = "FAILED"
	ExecutionCancelled ExecutionState = "CANCELLED"
	ExecutionExpired   ExecutionState = "EXPIRED"
	ExecutionRetried   ExecutionState = "RETRIED"
	ExecutionSuspended ExecutionState = "SUSPENDED"
)

func (state ExecutionState) Terminal() bool {
	switch state {
	case ExecutionSucceeded, ExecutionFailed, ExecutionCancelled,
		ExecutionExpired, ExecutionRetried, ExecutionSuspended:
		return true
	default:
		return false
	}
}

type ResourceClass string

const (
	ResourceStandard    ResourceClass = "STANDARD"
	ResourceHighMemory  ResourceClass = "HIGH_MEMORY"
	ResourceAccelerated ResourceClass = "ACCELERATED"
)

type AccessProfile string

const (
	AccessNone         AccessProfile = "NONE"
	AccessProjectRead  AccessProfile = "PROJECT_READ_ONLY"
	AccessClusterAdmin AccessProfile = "CLUSTER_ADMIN"
)

type IncidentKind string

const (
	IncidentHeartbeatMissed     IncidentKind = "HEARTBEAT_MISSED"
	IncidentReconcileFailed     IncidentKind = "RECONCILE_FAILED"
	IncidentWorkloadUnavailable IncidentKind = "WORKLOAD_UNAVAILABLE"
)
