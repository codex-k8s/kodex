package entity

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ManagedKubernetesInvocationLease is fresh bounded credit on the same existing outer invocation.
// ManagedKubernetesInvocationLease — fresh bounded credit того же existing outer invocation.
type ManagedKubernetesInvocationLease struct {
	Ref        string
	Generation int64
	ExpiresAt  time.Time
}

// ManagedKubernetesOperationSnapshot is a broker-only projection of owner-resolved immutable input.
// Credential fields are exact locators, never material; normalized input contains no Secret values.
// Resource/discovery addresses follow the exact registered execution class; Plan/Offline can omit both.
// ManagedKubernetesOperationSnapshot — broker-only проекция owner-resolved immutable input.
// Credential fields — exact locators, не material; normalized input не содержит Secret values.
// Resource/discovery addresses следуют exact registered execution class; Plan/Offline допускают оба пустыми.
type ManagedKubernetesOperationSnapshot struct {
	Digest, Operation, Risk, ApprovalPolicy, IdentityRef, SubjectDigest string
	IdentityGeneration                                                  int64
	Target                                                              value.ManagedKubernetesTarget
	ResourcePolicy                                                      *managedkubernetes.ResourcePolicy
	ResourcePolicySHA256                                                string
	ProtectedNamespaces                                                 []string
	IdentityLocator                                                     value.ManagedKubernetesIdentityLocator
	NormalizedInputJSON                                                 []byte
	Resource                                                            value.ManagedKubernetesResourceAddress
	Discovery                                                           *value.ManagedKubernetesDiscoveryAddress
	NodeTransport                                                       *value.ManagedKubernetesNodeTransport
	ExpiresAt                                                           time.Time
	ReconcileOnly                                                       bool
	PlanRef, PlanDigest                                                 string
	PlanRevision                                                        int64
}

// ManagedKubernetesChildEffect is one owner-assigned exact resource effect inside a finite plan.
// ManagedKubernetesChildEffect — один owner-assigned exact resource effect внутри finite plan.
type ManagedKubernetesChildEffect struct {
	Ref, Operation, InputDigest, State string
	Resource                           value.ManagedKubernetesResourceAddress
	NormalizedInputJSON                []byte
	NodeTransport                      *value.ManagedKubernetesNodeTransport
	Observation                        *managedkubernetes.ChildCandidate
}

// ManagedKubernetesPlan retains immutable child cardinality and semantics for repeat and rejoin.
// ManagedKubernetesPlan сохраняет immutable child cardinality/semantics для repeat и rejoin.
type ManagedKubernetesPlan struct {
	Ref, Digest string
	Revision    int64
	Children    []ManagedKubernetesChildEffect
}

// ManagedKubernetesStreamGrant reserves server-owned stream refs, channel sets and bounded credit.
// ManagedKubernetesStreamGrant резервирует server-owned stream refs, channel sets и bounded credit.
type ManagedKubernetesStreamGrant struct {
	Ref                           string
	Generation                    int64
	ExpiresAt                     time.Time
	MaximumFrameBytes             uint32
	MaximumTotalBytes             uint64
	InputChannels, OutputChannels []string
}

// ManagedKubernetesAcceptedEffectResult contains only a previously accepted bounded safe projection.
// A fresh Begin output deadline is mandatory before delivery; these bytes grant no new target action.
// ManagedKubernetesAcceptedEffectResult содержит только previously accepted bounded safe projection.
// До delivery нужен fresh Begin output deadline; эти bytes не разрешают новое target action.
type ManagedKubernetesAcceptedEffectResult struct {
	Outcome                  enum.ManagedKubernetesEffectOutcome
	ResultJSON               []byte
	ResultDigest, ReasonCode string
}

// ManagedKubernetesEffectAdmission is released only after durable ATTEMPTING or an exact readback.
// ReconcileOnly never grants another external mutation or handshake attempt.
// ManagedKubernetesEffectAdmission выдаётся после durable ATTEMPTING либо exact readback.
// ReconcileOnly не разрешает другую external mutation либо handshake attempt.
type ManagedKubernetesEffectAdmission struct {
	Ref, InputDigest string
	Generation       int64
	ExpiresAt        time.Time
	ReconcileOnly    bool
	Stream           *ManagedKubernetesStreamGrant
	AcceptedResult   *ManagedKubernetesAcceptedEffectResult
	History          *ManagedKubernetesStreamHistory
}

// ManagedKubernetesEffectReceipt exposes the stored terminal result binding without remote bytes.
// ManagedKubernetesEffectReceipt выдаёт stored terminal result binding без remote bytes.
type ManagedKubernetesEffectReceipt struct {
	State, ResultDigest, SubmissionDigest string
	AuthorizedUntil                       time.Time
	Applied                               bool
}

// ManagedKubernetesFrameAdmission separates new credit from replay-safe receipt readback.
// ManagedKubernetesFrameAdmission отделяет new credit от replay-safe receipt readback.
type ManagedKubernetesFrameAdmission struct {
	Disposition          enum.ManagedKubernetesFrameDisposition
	ExpiresAt            time.Time
	DeliveryOrdinal      uint32
	PortForwardRetention *managedkubernetes.PortForwardRetentionSet
}

// ManagedKubernetesStreamReceipt retains terminal local state and independently known remote outcome.
// ManagedKubernetesStreamReceipt сохраняет terminal local state и отдельно известный remote outcome.
type ManagedKubernetesStreamReceipt struct {
	State         enum.ManagedKubernetesStreamState
	RemoteOutcome enum.ManagedKubernetesEffectOutcome
}

// ManagedKubernetesStreamHistory retains original bounded evidence without fresh transport authority.
// A missing handshake is unreported evidence, never proof that no native handshake occurred.
// ManagedKubernetesStreamHistory хранит original bounded evidence без fresh transport authority.
// Missing handshake означает unreported evidence, не proof отсутствия native handshake.
type ManagedKubernetesStreamHistory struct {
	Ref          string
	Generation   int64
	State        enum.ManagedKubernetesStreamState
	HardDeadline time.Time
	Handshake    *managedkubernetes.HandshakeReceipt
	Terminal     *managedkubernetes.TerminalReceipt
}

// ManagedKubernetesCleanupReceipt permits only stopping the exact already closed original graph.
// It carries no credential locator, frame budget, output deadline or permission for a new effect.
// ManagedKubernetesCleanupReceipt допускает лишь остановку exact already closed original graph.
// Не содержит credential locator, frame budget, output deadline либо permission нового effect.
type ManagedKubernetesCleanupReceipt struct {
	InvocationRef, StreamRef, BindingDigest, SnapshotDigest string
	Generation                                              int64
	State                                                   enum.ManagedKubernetesStreamState
	ExecutionClosed                                         bool
}

// ManagedKubernetesInvocationReceipt binds terminal ACK/readback to the exact original claim.
// It contains no result bytes or output credit; unrecorded owner closure never implies business success.
// ManagedKubernetesInvocationReceipt связывает terminal ACK/readback с exact original claim.
// Не содержит result bytes либо output credit; unrecorded owner closure не означает business success.
type ManagedKubernetesInvocationReceipt struct {
	InvocationRef, InputDigest, BindingDigest string
	Generation                                int64
	State                                     enum.ManagedKubernetesInvocationState
	PlanRef, PlanDigest, SnapshotDigest       string
	PlanRevision                              int64
	ReceiptRef, ResultDigest                  string
	CompletionRecorded                        bool
	CompletionPendingReason                   enum.ManagedKubernetesCompletionPendingReason
}
