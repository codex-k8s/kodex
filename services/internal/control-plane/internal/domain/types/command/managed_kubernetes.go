package command

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"time"
)

// ManagedKubernetesChildCandidate carries scoped discovery metadata, never a chosen child actor or command.
// ManagedKubernetesChildCandidate хранит scoped discovery metadata, не выбранные child actor либо command.
type ManagedKubernetesChildCandidate = managedkubernetes.ChildCandidate

// ManagedKubernetesPrepareChildren proposes one finite child set for an immutable owner snapshot.
// ManagedKubernetesPrepareChildren предлагает один finite child set для immutable owner snapshot.
type ManagedKubernetesPrepareChildren struct {
	SnapshotDigest, ProposalDigest string
	Candidates                     []ManagedKubernetesChildCandidate
}

// ManagedKubernetesBeginEffect selects only a server-created child from a version-pinned owner plan.
// ManagedKubernetesBeginEffect выбирает только server-created child из version-pinned owner plan.
type ManagedKubernetesBeginEffect struct {
	PlanRef, PlanDigest, ChildRef, ChildInputDigest string
	PlanRevision                                    int64
	FileSourceRef                                   string
	FileSourceVersion                               int64
}

// ManagedKubernetesOpenStream records the first handshake without permitting another remote process.
// ManagedKubernetesOpenStream фиксирует первый handshake, не разрешая другой remote process.
type ManagedKubernetesOpenStream struct {
	EffectRef, StreamRef, HandshakeReceiptDigest string
	EffectGeneration                             int64
	HandshakeReceipt                             managedkubernetes.HandshakeReceipt
}

// ManagedKubernetesCompleteEffect submits bounded safe output for the exact external attempt.
// The owner validates result shape, digests and eligibility rather than trusting caller metadata.
// ManagedKubernetesCompleteEffect передаёт bounded safe output для exact external attempt.
// Owner проверяет result shape, digests и eligibility, не доверяя caller metadata.
type ManagedKubernetesCompleteEffect struct {
	EffectRef, ChildInputDigest, ResultDigest, ReasonCode, StreamRef string
	EffectGeneration                                                 int64
	Outcome                                                          enum.ManagedKubernetesEffectOutcome
	ResultJSON                                                       []byte
}

// ManagedKubernetesBeginFrame contains credit metadata only; payload never enters CP state.
// ManagedKubernetesBeginFrame содержит только credit metadata; payload не попадает в CP state.
type ManagedKubernetesBeginFrame struct {
	StreamRef, Channel, PayloadDigest string
	Direction                         enum.ManagedKubernetesFrameDirection
	Sequence                          uint64
	ByteCount                         uint32
	EndOfInput                        bool
	NativeAuthorizedUntil             time.Time
	PortForward                       *managedkubernetes.PortForwardConnectionWitness
}

// ManagedKubernetesCompleteFrame acknowledges one attempted sequence and its immutable payload digest.
// ManagedKubernetesCompleteFrame подтверждает одну attempted sequence и immutable payload digest.
type ManagedKubernetesCompleteFrame struct {
	StreamRef, PayloadDigest string
	Direction                enum.ManagedKubernetesFrameDirection
	Sequence                 uint64
}

// ManagedKubernetesCloseStream reports a closed registered transition; owner state decides whether
// it may complete, cancel or retain only late evidence after revocation or expiry.
// ManagedKubernetesCloseStream сообщает closed registered transition; owner state определяет,
// допустимы completion/cancel либо только late evidence после revocation/expiry.
type ManagedKubernetesCloseStream struct {
	StreamRef, TerminalReceiptDigest string
	ExpectedState, TerminalState     enum.ManagedKubernetesStreamState
	RemoteOutcome                    enum.ManagedKubernetesEffectOutcome
	TransportJoined                  bool
	ObservedExitCode                 *int32
	TerminalReceipt                  managedkubernetes.TerminalReceipt
	ObservedHandshake                *managedkubernetes.HandshakeReceipt
}
