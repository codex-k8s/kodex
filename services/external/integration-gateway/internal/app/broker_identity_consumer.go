package app

import (
	"context"
	"sync"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// brokerIdentityProbeReceipt reports purpose-owner completion, never generic credential verification authority or a Secret descriptor.
// Attempted remains observable even when a later completion acknowledgement is lost.
// brokerIdentityProbeReceipt сообщает purpose-owner completion без generic credential verification authority либо Secret descriptor.
// Attempted учитывается также при потере последующего completion acknowledgement.
type brokerIdentityProbeReceipt struct {
	TestRef                           string
	State                             string
	Applied, Attempted, Authenticated bool
}

// brokerIdentityInvocationProgress retains the exact original claim while its purpose consumer owns renewal and outer completion.
// Nonterminal states require an application-owned activity; returning from a unary request must not abandon it.
// State is RUNNING, TERMINAL, WAITING_OWNER, STREAM_ACTIVE or RECONCILING; none implies business success.
// RemoteOutcome separately reports SUCCEEDED, FAILED, UNKNOWN, RUNNING or NOT_STARTED.
// brokerIdentityInvocationProgress сохраняет exact original claim; purpose consumer владеет renewal и outer completion.
// Nonterminal states требуют application-owned activity; возврат unary request не оставляет её без владельца.
// State принимает RUNNING, TERMINAL, WAITING_OWNER, STREAM_ACTIVE либо RECONCILING; ни одно не означает business success.
// RemoteOutcome отдельно сообщает SUCCEEDED, FAILED, UNKNOWN, RUNNING либо NOT_STARTED.
// RUNNING acknowledges local adoption before the first RPC: RemoteOutcome=NOT_STARTED and Attempted=false.
// RUNNING подтверждает local adoption до первого RPC: RemoteOutcome=NOT_STARTED и Attempted=false.
// STREAM_ACTIVE permits only RUNNING outcome; RECONCILING only UNKNOWN; WAITING_OWNER permits NOT_STARTED or RUNNING.
// STREAM_ACTIVE допускает лишь RUNNING outcome, RECONCILING — UNKNOWN, WAITING_OWNER — NOT_STARTED либо RUNNING.
// TERMINAL requires a verified exact owner terminal state, from recorded finalization or a strict authenticated closed-state read, plus all local cleanup joined.
// TERMINAL требует verified exact owner terminal state из recorded finalization либо strict authenticated closed-state read и join всей local cleanup.
type brokerIdentityInvocationProgress struct {
	InvocationRef, LeaseRef, InputDigest, State, RemoteOutcome string
	Generation                                                 int64
	Attempted                                                  bool
}

// brokerIdentityConsumer receives already claimed work and never runs a competing Claim loop.
// Its purpose owner rechecks current lineage, material and deadline before effects and results.
// brokerIdentityConsumer получает уже claimed work и не запускает competing Claim loop.
// Purpose owner заново проверяет current lineage, material и deadline до effects и results.
type brokerIdentityConsumer interface {
	// Close synchronously closes admission and requests cancellation without waiting for network or declaring remote quiescence.
	// Close синхронно закрывает admission и запрашивает cancel без ожидания network либо утверждения remote quiescence.
	Close() error
	// Wait observes actual app-owned activity/cleanup join, preserving fatal lifecycle error after joined completion.
	// Wait наблюдает actual app-owned activity/cleanup join и сохраняет fatal lifecycle error после joined completion.
	Wait(context.Context) error
	// Joined distinguishes a positive local activity join from timeout or a joined failure; synchronous health calls belong to the common worker join.
	// Joined отличает positive local activity join от timeout либо joined failure; synchronous health calls принадлежат common worker join.
	Joined() bool
	// Failure closes once on fatal unresolved lifecycle failure, enabling the app-owned monitor to stop serving before shutdown; it is not a join receipt.
	// Failure закрывается once при fatal unresolved lifecycle failure и позволяет app-owned monitor остановить обслуживание до shutdown; это не join receipt.
	Failure() <-chan struct{}

	// ProbePersonal completes the personal test only through its purpose-specific owner.
	// ProbePersonal завершает personal test лишь через purpose-specific owner.
	ProbePersonal(context.Context, *cp.IntegrationConnectionTestClaim) (brokerIdentityProbeReceipt, error)
	// ProbeShared completes the shared test only through its purpose-specific owner.
	// ProbeShared завершает shared test лишь через purpose-specific owner.
	ProbeShared(context.Context, *cp.IntegrationConnectionTestClaim) (brokerIdentityProbeReceipt, error)
	// ReserveClaim reserves actual bounded local capacity before the owner's sole Claim RPC; absence excludes this purpose from that claim request.
	// ReserveClaim резервирует actual bounded local capacity до sole owner Claim RPC; отсутствие исключает purpose из этого claim request.
	ReserveClaim() (brokerIdentityClaimSlot, bool)
}

// brokerIdentityClaimSlot is one pre-claim reservation, adopted by at most one exact claim. It never mints authority or runs Claim itself.
// An adopted slot belongs to an application-owned activity through renew, gate, stream and outer completion; only terminal/positive join releases it.
// brokerIdentityClaimSlot является одной pre-claim reservation, принимающей не более одного exact claim. Authority и собственный Claim отсутствуют.
// Adopted slot принадлежит application-owned activity через renew, gate, stream и outer completion; освобождение требует terminal/positive join.
type brokerIdentityClaimSlot interface {
	// ExecuteClaim adopts once; ctx controls admission/handoff only, while activity lifetime comes from the consumer's app-owned constructor context.
	// ExecuteClaim принимает claim однократно; ctx управляет лишь admission/handoff, lifetime activity задаёт app-owned context constructor consumer.
	ExecuteClaim(context.Context, *cp.IntegrationInvocationClaim) (brokerIdentityInvocationProgress, error)
	// ReleaseUnused is idempotent and releases only a never-adopted reservation; it never cancels or releases an active activity, including after an uncertain response.
	// ReleaseUnused идемпотентно освобождает лишь never-adopted reservation; active activity не отменяется и не освобождается, включая uncertain response.
	ReleaseUnused()
}

// brokerIdentityConsumers is a fixed purpose registry; caller strings cannot register another transport or fallback.
// brokerIdentityConsumers является fixed purpose registry; caller strings не регистрируют иной transport либо fallback.
type brokerIdentityConsumers struct {
	Kubernetes brokerIdentityConsumer
	SSH        brokerIdentityConsumer
	// deliveryContract reads only the protected owner's code/schema contract; the trusted composition validates the actual typed wrapper and unknown fields before returning revision one.
	// deliveryContract читает лишь protected owner code/schema contract; trusted composition проверяет actual typed wrapper и unknown fields до возврата revision one.
	deliveryContract func(context.Context) (uint32, error)
	claimMutex       sync.Mutex
	claimTurn        uint8
	claimClosed      bool
}

// forPurpose accepts only the two declared purposes and never falls back to a generic token adapter.
// forPurpose принимает лишь два declared purposes без fallback к generic token adapter.
func (consumers *brokerIdentityConsumers) forPurpose(purpose cp.IntegrationBrokerIdentityPurpose) brokerIdentityConsumer {
	if consumers == nil {
		return nil
	}
	switch purpose {
	case cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_KUBERNETES_TARGET:
		return consumers.Kubernetes
	case cp.IntegrationBrokerIdentityPurpose_INTEGRATION_BROKER_IDENTITY_PURPOSE_SSH_TARGET:
		return consumers.SSH
	default:
		return nil
	}
}
