package kubernetesoperation

import (
	"context"
	"errors"
	"testing"
	"time"

	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// unusedStreamOwner refuses accidental owner I/O in lifecycle-only tests through a nil embedded port.
// unusedStreamOwner исключает случайный owner I/O в lifecycle-only проверках через nil embedded port.
type unusedStreamOwner struct{ targetrepo.StreamPlanOwner }

// unusedPodPreparer refuses private access in lifecycle-only tests through a nil embedded port.
// unusedPodPreparer исключает private access в lifecycle-only проверках через nil embedded port.
type unusedPodPreparer struct{ targetrepo.PodStreamPreparer }

// TestStreamRegistryRetainsDependenciesAfterFailedTransportJoin uses actual active workers and a failed close.
// Worker termination must not turn an unproven native transport join into permission to close dependencies.
// TestStreamRegistryRetainsDependenciesAfterFailedTransportJoin использует реальные active workers и ошибку Close.
// Завершение workers не превращает недоказанный native join в разрешение закрыть зависимости.
func TestStreamRegistryRetainsDependenciesAfterFailedTransportJoin(t *testing.T) {
	stream, _, _, _, _ := activeStreamFixture(t, func(_ *activeOwnerFixture, _ *activeAccessFixture, transport *activeTransportFixture, _ *value.KubernetesStreamGrant) {
		transport.closeErr = errors.New("synthetic unresolved native join")
	})
	service, err := NewStreamOperations(t.Context(), unusedStreamOwner{}, unusedPodPreparer{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	entry, release, err := service.reserve("synthetic", stream.binding)
	if err != nil {
		t.Fatal(err)
	}
	entry.active = stream
	go service.monitor("synthetic", entry, release)
	stream.stop("LOST", "TRANSPORT_LOST")
	select {
	case <-entry.done:
	case <-time.After(time.Second):
		t.Fatal("local workers did not terminate")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if service.Close(ctx) == nil || service.Joined() {
		t.Fatal("failed native join released dependency guard")
	}
	if service.Close(ctx) == nil || service.Joined() {
		t.Fatal("repeated cleanup erased unresolved native join")
	}
}

// TestStreamFailedOpeningDoesNotForgeJoin prevents late completion evidence after an unsuccessful native close.
// TestStreamFailedOpeningDoesNotForgeJoin запрещает late completion evidence после неуспешного native Close.
func TestStreamFailedOpeningDoesNotForgeJoin(t *testing.T) {
	stream, _, _, transport, _ := activeStreamFixture(t, func(_ *activeOwnerFixture, _ *activeAccessFixture, transport *activeTransportFixture, _ *value.KubernetesStreamGrant) {
		transport.closeErr = errors.New("synthetic unresolved native join")
	})
	service, err := NewStreamOperations(t.Context(), unusedStreamOwner{}, unusedPodPreparer{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	service.closeFailedStream(stream.binding, transport, failedOpenedStream(stream.handshake), &stream.handshake, "ACTIVE")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if service.Close(ctx) == nil || service.Joined() {
		t.Fatal("failed opening fabricated positive transport join")
	}
}
