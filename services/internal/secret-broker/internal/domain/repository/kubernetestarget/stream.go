package kubernetestarget

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// StreamOpening binds one admitted effect and its actual first handshake receipt to a prepared grant.
// StreamOpening связывает admitted effect и actual first handshake receipt с prepared grant.
type StreamOpening struct {
	EffectRef, HandshakeReceiptDigest string
	EffectGeneration                  int64
	Prepared                          value.KubernetesStreamGrant
	HandshakeReceipt                  managedkubernetes.HandshakeReceipt
}

// StreamClosing records local writer join separately from independently observed remote outcome.
// StreamClosing сохраняет local writer join отдельно от независимо наблюдённого remote outcome.
type StreamClosing struct {
	StreamRef, ExpectedState, TerminalState, RemoteOutcome, TerminalReceiptDigest string
	TransportJoined                                                               bool
	ObservedExitCode                                                              *int32
	TerminalReceipt                                                               managedkubernetes.TerminalReceipt
	// ObservedHandshake carries late native facts after an uncertain owner Open reply, never new frame credit.
	// ObservedHandshake передаёт late native facts после uncertain owner Open reply без нового frame credit.
	ObservedHandshake *managedkubernetes.HandshakeReceipt
}

// StreamReceipt is authoritative terminal readback; cancellation/revocation may supersede completion.
// StreamReceipt содержит authoritative terminal readback; cancellation/revocation могут заменить completion.
type StreamReceipt struct{ State, RemoteOutcome string }

// StreamOwner records one handshake and terminal evidence without opening or replaying a transport.
// StreamOwner сохраняет один handshake и terminal evidence без открытия либо replay transport.
type StreamOwner interface {
	// OpenKubernetesStream accepts only the first observed handshake under exact owner effect credit.
	// OpenKubernetesStream принимает только первый наблюдённый handshake с exact owner effect credit.
	OpenKubernetesStream(context.Context, managedkubernetes.Binding, StreamOpening) (value.KubernetesStreamGrant, error)
	// CloseKubernetesStream resolves final state after transport join without inventing a remote exit.
	// CloseKubernetesStream разрешает final state после transport join без выдуманного remote exit.
	CloseKubernetesStream(context.Context, managedkubernetes.Binding, StreamClosing) (StreamReceipt, error)
}

// StreamCleanupReceipt proves an irreversible owner closure for one original stream, without output or frame credit.
// StreamCleanupReceipt подтверждает необратимое owner closure исходного stream без output либо frame credit.
type StreamCleanupReceipt struct {
	InvocationRef, StreamRef, BindingDigest, SnapshotDigest, StreamState string
	Generation                                                           int64
	ExecutionClosed                                                      bool
}

// StreamCleanupResolver reads closure through the existing owner graph without selecting credentials or issuing grants.
// StreamCleanupResolver читает closure через existing owner graph без выбора credentials либо выдачи grants.
type StreamCleanupResolver interface {
	// ResolveKubernetesStreamCleanup accepts only the original exact binding and a previously owned stream.
	// ResolveKubernetesStreamCleanup принимает лишь original exact binding и ранее owned stream.
	ResolveKubernetesStreamCleanup(context.Context, managedkubernetes.Binding, string) (StreamCleanupReceipt, error)
}
