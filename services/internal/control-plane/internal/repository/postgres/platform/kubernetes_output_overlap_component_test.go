package platform

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesOutputOverlapMarker identifies only the first completed historical metadata query.
// kubernetesOutputOverlapMarker отмечает лишь первый completed historical metadata query.
type kubernetesOutputOverlapMarker struct{}

// kubernetesOutputOverlapTrace holds the old metadata snapshot while another connection advances
// the actual OUTPUT ordinal. No production statement or trusted owner predicate is replaced.
// kubernetesOutputOverlapTrace удерживает old metadata snapshot, пока другое соединение меняет
// actual OUTPUT ordinal. Production statement и trusted owner predicate не подменяются.
type kubernetesOutputOverlapTrace struct {
	fired   atomic.Bool
	ready   chan struct{}
	release chan struct{}
}

// TraceQueryStart marks one exact metadata read and never mutates the database or result model.
// TraceQueryStart помечает один exact metadata read без изменения БД либо result model.
func (trace *kubernetesOutputOverlapTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == queryKubernetesOutputRead && trace.fired.CompareAndSwap(false, true) {
		return context.WithValue(ctx, kubernetesOutputOverlapMarker{}, true)
	}
	return ctx
}

// TraceQueryEnd exposes a barrier after PostgreSQL has produced the real old metadata snapshot.
// TraceQueryEnd открывает barrier после возврата actual old metadata snapshot из PostgreSQL.
func (trace *kubernetesOutputOverlapTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if marked, _ := ctx.Value(kubernetesOutputOverlapMarker{}).(bool); !marked || data.Err != nil {
		return
	}
	trace.ready <- struct{}{}
	select {
	case <-trace.release:
	case <-ctx.Done():
	}
}

// kubernetesOutputOverlapResult carries only safe metadata/error while joining the one reader.
// kubernetesOutputOverlapResult передаёт safe metadata/error при join единственного reader.
type kubernetesOutputOverlapResult struct {
	receipt entity.ManagedKubernetesOutputFrameReceipt
	err     error
}

// assertKubernetesOutputOrdinalOverlap proves that completed delivery2 invalidates an old accepted
// metadata snapshot held on another connection. The loser may return no credit or an infrastructure
// serialization error; it cannot return ACCEPTED for ordinal1 or perform any PUT.
// assertKubernetesOutputOrdinalOverlap доказывает, что completed delivery2 отменяет old accepted
// metadata snapshot другого соединения. Допустим no credit либо serialization error; ACCEPTED
// для ordinal1 и новый PUT запрещены.
func assertKubernetesOutputOrdinalOverlap(t *testing.T, ctx context.Context, repository *Repository, broker value.Principal, binding value.ManagedKubernetesBinding, output command.ManagedKubernetesBeginFrame) entity.ManagedKubernetesFrameAdmission {
	t.Helper()
	work, cancel := context.WithTimeout(ctx, 7*time.Second)
	trace := &kubernetesOutputOverlapTrace{ready: make(chan struct{}, 1), release: make(chan struct{})}
	var once sync.Once
	unblock := func() { once.Do(func() { close(trace.release) }) }
	config := repository.pool.Config()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(work, config)
	if err != nil {
		cancel()
		t.Fatal("construct native output overlap pool", err)
	}
	reader := *repository
	reader.pool = pool
	results := make(chan kubernetesOutputOverlapResult, 1)
	joined := false
	defer func() {
		unblock()
		cancel()
		if !joined {
			select {
			case <-results:
			case <-time.After(2 * time.Second):
				t.Error("native output metadata reader did not join")
			}
		}
		pool.Close()
	}()
	gateway := resolvedTestPrincipal(t, work, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.claim"}, "integration-gateway")
	gateway, err = repository.ResolvePrincipal(work, gateway)
	if err != nil {
		t.Fatal("resolve overlap gateway", err)
	}
	gateway.Permission = "platform.runtime.kubernetes.output.read"
	go func() {
		receipt, err := reader.GetManagedKubernetesOutputFrame(work, gateway, binding, command.ManagedKubernetesOutputFrameLookup{StreamRef: output.StreamRef, Sequence: output.Sequence, DeliveryOrdinal: 1})
		results <- kubernetesOutputOverlapResult{receipt: receipt, err: err}
	}()
	select {
	case <-trace.ready:
	case result := <-results:
		joined = true
		t.Fatal("output read never reached real metadata barrier", result.err)
	case <-work.Done():
		t.Fatal("output metadata barrier timed out")
	}
	output.NativeAuthorizedUntil = time.Now().Add(4 * time.Second)
	admission, err := repository.BeginManagedKubernetesFrame(work, broker, binding, output)
	if err != nil || admission.Disposition != enum.KubernetesFrameSend || admission.DeliveryOrdinal != 2 {
		t.Fatal("actual concurrent OUTPUT ordinal did not advance", err)
	}
	unblock()
	select {
	case result := <-results:
		joined = true
		if result.err != nil && !errors.Is(result.err, errs.ErrUnavailable) {
			t.Fatal("unexpected output overlap denial", result.err)
		}
		if result.err == nil && (result.receipt.State == enum.KubernetesOutputAccepted || !result.receipt.AuthorizedUntil.IsZero()) {
			t.Fatal("stale metadata snapshot retained old OUTPUT credit")
		}
	case <-work.Done():
		t.Fatal("output metadata reader failed to join")
	}
	return admission
}
