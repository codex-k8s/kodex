package kubernetestarget

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// EffectOpening identifies an owner-assigned child and the current snapshot's maximum lifetime.
// EffectOpening указывает назначенный владельцем child и предельный срок текущего snapshot.
type EffectOpening struct {
	PlanRef, PlanDigest, ChildRef, ChildInputDigest, Operation string
	PlanRevision                                               int64
	ExpiresAt                                                  time.Time
	// HardStreamDeadline is derived from immutable runtime/input/node limits, separately from the dispatch lease.
	// HardStreamDeadline выводится из immutable runtime/input/node limits отдельно от dispatch lease.
	HardStreamDeadline time.Time
	FileSourceRef      string
	FileSourceVersion  int64
}

// EffectAdmission separates a pending gate and reconciliation from one fresh dispatch credit.
// EffectAdmission отделяет pending gate и reconciliation от допуска одного нового запроса.
type EffectAdmission struct {
	EffectRef, ChildInputDigest, GateRef string
	EffectGeneration                     int64
	ExpiresAt                            time.Time
	ReconcileOnly                        bool
	Stream                               *value.KubernetesStreamGrant
	AcceptedResult                       *AcceptedEffectResult `json:"-"`
	History                              *StreamHistory        `json:"-"`
}

// StreamHistory contains original owner evidence, never a replacement native stream or output grant.
// StreamHistory содержит исходное owner evidence, не replacement native stream либо output grant.
type StreamHistory struct {
	Ref, State   string
	Generation   int64
	HardDeadline time.Time
	Handshake    *managedkubernetes.HandshakeReceipt
	Terminal     *managedkubernetes.TerminalReceipt
}

// EffectCompletion carries only a previously sanitized bounded result, never private material.
// EffectCompletion передаёт только ранее очищенный ограниченный результат без private material.
type EffectCompletion struct {
	EffectRef, ChildInputDigest, Outcome, ReasonCode, StreamRef string
	EffectGeneration                                            int64
	ResultJSON                                                  []byte `json:"-"`
}

// String prevents diagnostic formatting from exposing business data in a result projection.
// String предотвращает раскрытие business data из проекции результата при диагностике.
func (EffectCompletion) String() string { return "[managed Kubernetes effect completion]" }

// GoString applies the same protection to detailed diagnostic formatting.
// GoString сохраняет ту же защиту для подробного диагностического форматирования.
func (input EffectCompletion) GoString() string { return input.String() }

// EffectReceipt preserves the owner's terminal override without creating further dispatch authority.
// EffectReceipt сохраняет terminal override владельца без нового допуска внешнего запроса.
type EffectReceipt struct {
	State, ResultDigest, SubmissionDigest string
	Applied                               bool
	AuthorizedUntil                       time.Time
}

// AcceptedEffectResult carries stored sanitized bytes under fresh output credit, never dispatch authority.
// AcceptedEffectResult содержит сохранённые sanitized bytes под fresh output credit, не dispatch authority.
type AcceptedEffectResult struct {
	Outcome, ReasonCode, ResultDigest string
	ResultJSON                        []byte `json:"-"`
}

// String prevents recovery data from entering diagnostic output.
// String исключает recovery data из diagnostic output.
func (AcceptedEffectResult) String() string { return "[managed Kubernetes accepted result]" }

// GoString protects detailed diagnostic formatting as well.
// GoString защищает также подробное diagnostic formatting.
func (result AcceptedEffectResult) GoString() string { return result.String() }

// EffectOwner admits one child attempt and records its result through the original owner binding.
// EffectOwner допускает одну child attempt и сохраняет результат через исходный owner binding.
type EffectOwner interface {
	// BeginKubernetesEffect never retries uncertain admission and returns no credit for a pending gate.
	// BeginKubernetesEffect не повторяет неопределённый admission и не даёт credit pending gate.
	BeginKubernetesEffect(context.Context, managedkubernetes.Binding, EffectOpening) (EffectAdmission, error)
	// CompleteKubernetesEffect records late evidence without extending or renewing the original lease.
	// CompleteKubernetesEffect сохраняет позднее evidence без продления исходного lease.
	CompleteKubernetesEffect(context.Context, managedkubernetes.Binding, EffectCompletion) (EffectReceipt, error)
}
