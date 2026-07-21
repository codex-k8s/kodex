package integrations

import "time"

const (
	CapabilityRestartWorkload = "deployment.restart_workload"
	CapabilityVersion         = 1
	ExecutorKindRecordingTest = "recording_test"
	InstallationScope         = "single-installation"
	SubjectKindAgentRole      = "agent_role"
)

// InvocationStatus задаёт монотонное состояние опасного вызова.
type InvocationStatus string

const (
	InvocationStatusPending   InvocationStatus = "pending"
	InvocationStatusApproved  InvocationStatus = "approved"
	InvocationStatusRejected  InvocationStatus = "rejected"
	InvocationStatusExpired   InvocationStatus = "expired"
	InvocationStatusCancelled InvocationStatus = "cancelled"
	InvocationStatusExecuting InvocationStatus = "executing"
	InvocationStatusSucceeded InvocationStatus = "succeeded"
	InvocationStatusFailed    InvocationStatus = "failed"
)

// ApprovalDecision задаёт допустимое решение человеко-ориентированной границы.
type ApprovalDecision string

const (
	ApprovalDecisionApprove ApprovalDecision = "approve"
	ApprovalDecisionReject  ApprovalDecision = "reject"
)

// SessionContext содержит только server-resolved идентификаторы авторизованной AgentSession.
type SessionContext struct {
	SessionID             int64
	SessionKey            string
	TurnID                int64
	ProjectID             int64
	ChatID                int64
	RoleID                int64
	SubjectKind           string
	SubjectRef            string
	InstallationScope     string
	WorkspaceScope        string
	MattermostChannelID   string
	MattermostRootPostID  string
	ApproverUserID        string
	ApproverUserName      string
	SessionTokenSecretRef string
}

// RestartWorkloadInput является единственным типизированным входом опасной capability этого среза.
type RestartWorkloadInput struct {
	Connection     string `json:"connection"`
	Namespace      string `json:"namespace"`
	WorkloadKind   string `json:"workload_kind"`
	WorkloadName   string `json:"workload_name"`
	IdempotencyKey string `json:"idempotency_key"`
}

// RestartArguments — канонизируемая безопасная часть входа без ключа повтора.
type RestartArguments struct {
	Namespace    string `json:"namespace"`
	WorkloadKind string `json:"workload_kind"`
	WorkloadName string `json:"workload_name"`
}

// ExecutionResult — сохранённый безопасный результат PostgreSQL recording executor.
type ExecutionResult struct {
	ExecutionID  string `json:"execution_id"`
	Namespace    string `json:"namespace"`
	WorkloadKind string `json:"workload_kind"`
	WorkloadName string `json:"workload_name"`
	RecordedAt   string `json:"recorded_at"`
}

// ToolResult — структурированный MCP-результат без внутренних PK и credential refs.
type ToolResult struct {
	Status           InvocationStatus `json:"status"`
	InvocationID     string           `json:"invocation_id"`
	ApprovalID       string           `json:"approval_id"`
	ArgumentsHash    string           `json:"arguments_hash"`
	ReasonCode       string           `json:"reason_code,omitempty"`
	PollAfterSeconds int              `json:"poll_after_seconds,omitempty"`
	Execution        *ExecutionResult `json:"execution,omitempty"`
}

// CatalogEntry описывает разрешённый текущей сессии инструмент без конфигурации соединения.
type CatalogEntry struct {
	CapabilityKey string
	Version       int
}

// Binding фиксирует точные ревизии connection/capability/grant для T1 и повторов.
type Binding struct {
	CapabilityID       int64
	CapabilityPublicID string
	CapabilityKey      string
	CapabilityVersion  int
	CapabilityRevision int64
	ConnectionID       int64
	ConnectionPublicID string
	ConnectionRevision int64
	GrantID            int64
	GrantPublicID      string
	GrantRevision      int64
}

// Invocation — безопасная проекция сохранённого вызова.
type Invocation struct {
	ID                  int64
	PublicID            string
	ApprovalID          int64
	ApprovalPublicID    string
	Status              InvocationStatus
	ReasonCode          string
	Arguments           RestartArguments
	ArgumentsHash       string
	ApprovalBindingHash string
	CorrelationID       string
	MattermostPostID    string
	Execution           *ExecutionResult
}

// ApprovalDelivery содержит только данные, необходимые для безопасной карточки.
type ApprovalDelivery struct {
	ApprovalID          int64
	ApprovalPublicID    string
	InvocationPublicID  string
	CapabilityKey       string
	ConnectionPublicID  string
	Arguments           RestartArguments
	ArgumentsHash       string
	ApprovalBindingHash string
	RiskClass           string
	ApproverUserID      string
	ApproverUserName    string
	WorkspaceScope      string
	SessionScope        string
	ChannelID           string
	RootPostID          string
	PostID              string
	ExpiresAt           time.Time
	DeliveryLeaseOwner  string
}

// ApprovalDecisionInput связывает callback с точным человеком, post и digest.
type ApprovalDecisionInput struct {
	ApprovalPublicID    string
	ApprovalBindingHash string
	Decision            ApprovalDecision
	ActorUserID         string
	ActorUserName       string
	ChannelID           string
	PostID              string
	Now                 time.Time
}

// ExecutionClaim — T3 fence для одного invocation.
type ExecutionClaim struct {
	InvocationID       int64
	InvocationPublicID string
	ExecutionFence     string
	Arguments          RestartArguments
	ArgumentsHash      string
	LeaseOwner         string
}

// ExecutionReceipt — неизменяемая квитанция единственного recording side effect.
type ExecutionReceipt struct {
	InvocationID   int64
	ExecutionFence string
	ArgumentsHash  string
	Result         ExecutionResult
}
