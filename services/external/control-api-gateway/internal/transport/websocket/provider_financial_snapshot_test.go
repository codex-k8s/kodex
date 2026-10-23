package websockettransport

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// financialSnapshotRecorder counts separate synthetic financial-summary and provider-catalog reads, returning a configured catalog error without hiding it behind the summary fixture.
// financialSnapshotRecorder считает отдельные тестовые чтения финансовой сводки и каталога провайдера, возвращая настроенную ошибку каталога без её скрытия сводкой.
type financialSnapshotRecorder struct {
	assistantSnapshotSizeRecorder
	catalogError                 error
	financialReads, catalogReads int
}

// Invoke requires an unpaginated summary-only request before supplying synthetic display metadata; catalog reads return the configured error and unrelated calls delegate to the assistant fixture.
// Invoke требует запрос только сводки без пагинации перед тестовой display metadata; каталог возвращает настроенную ошибку, остальные вызовы передаются оснастке помощника.
func (c *financialSnapshotRecorder) Invoke(ctx context.Context, method string, request, response any, opts ...grpc.CallOption) error {
	if out, ok := response.(*cp.ListProviderAccountsResponse); ok {
		input := request.(*cp.ListProviderAccountsRequest)
		if input.FinancialSummaryOnly {
			c.financialReads++
			if input.Page != nil || input.Query != "" {
				return status.Error(codes.InvalidArgument, "financial read inherited pagination")
			}
			now := time.Now().UTC()
			out.FinancialSummary = &cp.ProviderFinancialOverview{Accounts: []*cp.ProviderFinancialDisplayAccount{{Ref: "pac_fixture01", Name: "Fixture"}}, Total: 1, Unknown: 1, ObservedAt: timestamppb.New(now), ValidUntil: timestamppb.New(now.Add(5 * time.Minute))}
			return nil
		}
		c.catalogReads++
		return c.catalogError
	}
	return c.assistantSnapshotSizeRecorder.Invoke(ctx, method, request, response, opts...)
}

// TestFinancialSnapshotOversizedCatalogKeepsOtherKinds supplies the exact typed oversized-catalog outcome and requires one bounded financial summary plus unaffected assistant bootstrap, without retrying the catalog.
// TestFinancialSnapshotOversizedCatalogKeepsOtherKinds подставляет точный типизированный исход чрезмерного каталога и требует одну ограниченную финансовую сводку с неизменным bootstrap помощника без повтора каталога.
func TestFinancialSnapshotOversizedCatalogKeepsOtherKinds(t *testing.T) {
	oversized, _ := status.New(codes.Unavailable, "provider catalog too large").WithDetails(&errdetails.ErrorInfo{Domain: "kodex.control-plane", Reason: "PROVIDER_CATALOG_TOO_LARGE"})
	c := &financialSnapshotRecorder{catalogError: oversized.Err()}
	m := assistantSizeMultiplexer(t, &c.assistantSnapshotSizeRecorder, "prj_fixture01")
	m.server.query = cp.NewPlatformQueryServiceClient(c)
	kinds, err := m.sendPlatformBootstrap()
	if err != nil {
		t.Fatal(err)
	}
	found := map[generated.PlatformResourceKind]bool{}
	for _, kind := range kinds {
		found[kind] = true
	}
	if !found["PROVIDER_ACCOUNT"] || !found["SYSTEM_ASSISTANT"] || c.financialReads != 1 || c.catalogReads != 1 {
		t.Fatal("oversized provider catalog removed another kind or retried a read")
	}
	var financial bool
	for len(c.frames) > 0 {
		frame := (<-c.frames).value.(generated.PlatformSnapshotEnvelope)
		if frame.Kind != "PROVIDER_ACCOUNT" {
			continue
		}
		b, err := json.Marshal(frame)
		if err != nil || len(b) > maximumFrameBytes || frame.Snapshot.Catalog.FinancialSummary == nil || !frame.Snapshot.Catalog.ProviderCatalogUnavailable {
			t.Fatal("bounded protected financial summary was lost")
		}
		financial = true
	}
	if !financial {
		t.Fatal("provider summary frame missing")
	}
}

// TestFinancialSnapshotDoesNotSuppressArbitraryFailure preserves generic unavailability, permission and authentication status instead of interpreting them as the optional typed oversized-catalog outcome.
// TestFinancialSnapshotDoesNotSuppressArbitraryFailure сохраняет общую недоступность, запрет и отказ входа, не превращая их в необязательный типизированный исход чрезмерного каталога.
func TestFinancialSnapshotDoesNotSuppressArbitraryFailure(t *testing.T) {
	for _, failure := range []error{status.Error(codes.Unavailable, "network failure"), status.Error(codes.PermissionDenied, "owner denial"), status.Error(codes.Unauthenticated, "auth failure")} {
		c := &financialSnapshotRecorder{catalogError: failure}
		m := assistantSizeMultiplexer(t, &c.assistantSnapshotSizeRecorder, "")
		m.server.query = cp.NewPlatformQueryServiceClient(c)
		if _, err := m.server.projectPlatformSnapshot(t.Context(), "PROVIDER_ACCOUNT", "", m.localize); status.Code(err) != status.Code(failure) {
			t.Fatal("arbitrary provider failure became optional unavailable")
		}
	}
}
