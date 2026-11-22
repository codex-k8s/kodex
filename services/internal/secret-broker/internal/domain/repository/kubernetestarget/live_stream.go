package kubernetestarget

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// LiveStream owns one already-admitted physical transport, never a factory or retry callback.
// LiveStream владеет одним already-admitted physical transport, не factory либо retry callback.
type LiveStream interface {
	FrameWriter
	// ReadStream transfers ownership of bounded output; Close must unblock pending I/O; the ActiveStream owner joins its reader and writer.
	// ReadStream передаёт ownership bounded output; Close разблокирует pending I/O; владелец ActiveStream ожидает reader и writer.
	ReadStream() (value.KubernetesStreamRead, error)
	// Close terminates local I/O only and never asserts that a remote process stopped.
	// Close завершает только local I/O и не утверждает остановку remote process.
	Close() error
}

// LiveStreamOwner shares the existing frame ledger and stream lifecycle without parallel authority.
// LiveStreamOwner объединяет existing frame ledger и stream lifecycle без parallel authority.
type LiveStreamOwner interface {
	FrameOwner
	StreamOwner
}

// StreamAccess rechecks current owner graph, selected identity TTL and exact target authorization.
// Returned credit expires no later than the current owner lease and selected credential validity.
// StreamAccess перепроверяет current owner graph, selected identity TTL и exact target authorization.
// Returned credit заканчивается не позднее current owner lease и selected credential validity.
type StreamAccess interface {
	// AuthorizeStream cannot substitute credentials, change an origin or reopen the physical transport.
	// AuthorizeStream не подменяет credentials, не меняет origin и не открывает physical transport заново.
	AuthorizeStream(context.Context, managedkubernetes.Binding, managedkubernetes.StreamReceiptOrigin) (time.Time, error)
}

// StreamTargetAccess rechecks the already-bound target using only its selected private identity.
// It cannot open another stream, replace credentials or infer authority from a previous health test.
// StreamTargetAccess перепроверяет уже привязанный target только с выбранной private identity.
// Не открывает другой stream, не меняет credentials и не выводит authority из прежнего health test.
type StreamTargetAccess interface {
	// VerifyKubernetesStreamAccess returns the actual credential/pin deadline after fresh target checks.
	// VerifyKubernetesStreamAccess возвращает actual credential/pin deadline после fresh target checks.
	VerifyKubernetesStreamAccess(context.Context, value.KubernetesOperationSnapshot) (time.Time, error)
}
