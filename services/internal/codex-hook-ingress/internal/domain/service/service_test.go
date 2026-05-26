package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	hookerrs "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/entity"
	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
	hookstub "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/repository/stub/hook"
	opsstub "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/repository/stub/ops"
)

func TestSubmitHookEventAcceptsSafeEnvelope(t *testing.T) {
	t.Parallel()

	service := newTestService()
	result, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: validPreToolUseEnvelope()})
	if err != nil {
		t.Fatalf("SubmitHookEvent(): %v", err)
	}
	if result.HandlerResult.Result != hookenum.HandlerResultContinue {
		t.Fatalf("result = %s, want continue", result.HandlerResult.Result)
	}
	if result.RoutesAccepted != 4 {
		t.Fatalf("routes accepted = %d, want 4", result.RoutesAccepted)
	}
}

func TestSubmitHookEventDispatchesSelectedSafeRoutes(t *testing.T) {
	t.Parallel()

	agentRoute := &recordingOwnerRoute{}
	runtimeRoute := &recordingOwnerRoute{}
	service := newTestServiceWithConfig(Config{}, testRouteRegistry(map[hookenum.DownstreamOwner]OwnerRoute{
		hookenum.DownstreamOwnerAgentManager:   agentRoute,
		hookenum.DownstreamOwnerRuntimeManager: runtimeRoute,
	}))
	envelope := validPreToolUseEnvelope()
	envelope.DownstreamRoutes = []value.DownstreamRoute{
		{
			Owner:        hookenum.DownstreamOwnerAgentManager,
			DeliveryMode: hookenum.DeliveryModeAsync,
			SafeParts:    []string{"source_context", "run_context", "tool_context", "correlation_id"},
		},
		{
			Owner:        hookenum.DownstreamOwnerRuntimeManager,
			DeliveryMode: hookenum.DeliveryModeAsync,
			SafeParts:    []string{"run_context", "risk_class"},
		},
	}

	result, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if err != nil {
		t.Fatalf("SubmitHookEvent(): %v", err)
	}
	if result.RoutesAccepted != 4 {
		t.Fatalf("routes accepted = %d, want 4", result.RoutesAccepted)
	}
	agentEvents := agentRoute.Events()
	if len(agentEvents) != 1 {
		t.Fatalf("agent route events = %d, want 1", len(agentEvents))
	}
	if agentEvents[0].SourceContext == nil || agentEvents[0].RunContext == nil || agentEvents[0].ToolContext == nil || agentEvents[0].CorrelationID == "" {
		t.Fatalf("agent route event did not receive selected safe parts: %+v", agentEvents[0])
	}
	runtimeEvents := runtimeRoute.Events()
	if len(runtimeEvents) != 1 {
		t.Fatalf("runtime route events = %d, want 1", len(runtimeEvents))
	}
	if runtimeEvents[0].RunContext == nil || runtimeEvents[0].RiskClass != "low" {
		t.Fatalf("runtime route event did not receive selected safe parts: %+v", runtimeEvents[0])
	}
}

func TestSubmitHookEventReportsDisabledRouteWithoutDispatch(t *testing.T) {
	t.Parallel()

	route := &recordingOwnerRoute{}
	service := newTestServiceWithConfig(
		Config{DisabledRoutes: []hookenum.DownstreamOwner{hookenum.DownstreamOwnerOperationsFeed}},
		testRouteRegistry(map[hookenum.DownstreamOwner]OwnerRoute{
			hookenum.DownstreamOwnerOperationsFeed: route,
		}),
	)

	result, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: validPreToolUseEnvelope()})
	if err != nil {
		t.Fatalf("SubmitHookEvent(): %v", err)
	}
	if result.RoutesAccepted != 3 {
		t.Fatalf("routes accepted = %d, want 3", result.RoutesAccepted)
	}
	if !hasRouteStatus(result.RouteDiagnostics, hookenum.DownstreamOwnerOperationsFeed, hookenum.RouteDeliveryStatusDisabled) {
		t.Fatalf("route diagnostics = %+v, want disabled", result.RouteDiagnostics)
	}
	if len(route.Events()) != 0 {
		t.Fatalf("disabled route received %d events, want 0", len(route.Events()))
	}
}

func TestSubmitHookEventReportsUnsupportedRoute(t *testing.T) {
	t.Parallel()

	service := newTestServiceWithConfig(Config{}, NewRouteRegistry(map[hookenum.DownstreamOwner]OwnerRoute{}))
	result, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: validPreToolUseEnvelope()})
	if err != nil {
		t.Fatalf("SubmitHookEvent(): %v", err)
	}
	if result.RoutesAccepted != 0 {
		t.Fatalf("routes accepted = %d, want 0", result.RoutesAccepted)
	}
	if len(result.RouteDiagnostics) != 4 {
		t.Fatalf("route diagnostics = %+v, want four canonical unsupported routes", result.RouteDiagnostics)
	}
	for _, diagnostic := range result.RouteDiagnostics {
		if diagnostic.Status != hookenum.RouteDeliveryStatusUnsupported {
			t.Fatalf("route diagnostics = %+v, want unsupported", result.RouteDiagnostics)
		}
	}
}

func TestSubmitHookEventReportsDownstreamFailureSafely(t *testing.T) {
	t.Parallel()

	service := newTestServiceWithConfig(Config{}, testRouteRegistry(map[hookenum.DownstreamOwner]OwnerRoute{
		hookenum.DownstreamOwnerOperationsFeed: failingOwnerRoute{err: errors.New("sensitive downstream detail")},
	}))

	result, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: validPreToolUseEnvelope()})
	if err != nil {
		t.Fatalf("SubmitHookEvent(): %v", err)
	}
	if result.RoutesAccepted != 3 {
		t.Fatalf("routes accepted = %d, want 3", result.RoutesAccepted)
	}
	if !hasRouteStatus(result.RouteDiagnostics, hookenum.DownstreamOwnerOperationsFeed, hookenum.RouteDeliveryStatusFailed) {
		t.Fatalf("route diagnostics = %+v, want failed", result.RouteDiagnostics)
	}
	diagnostic := diagnosticForOwner(result.RouteDiagnostics, hookenum.DownstreamOwnerOperationsFeed).DiagnosticMessage
	if strings.Contains(diagnostic, "sensitive downstream detail") {
		t.Fatalf("diagnostic leaked downstream error: %q", diagnostic)
	}
}

func TestSubmitHookEventRejectsSenderControlledUnexpectedRoute(t *testing.T) {
	t.Parallel()

	providerRoute := &recordingOwnerRoute{}
	service := newTestServiceWithConfig(Config{}, testRouteRegistry(map[hookenum.DownstreamOwner]OwnerRoute{
		hookenum.DownstreamOwnerProviderHub: providerRoute,
	}))
	envelope := validPreToolUseEnvelope()
	envelope.DownstreamRoutes = append(envelope.DownstreamRoutes, value.DownstreamRoute{
		Owner:        hookenum.DownstreamOwnerProviderHub,
		DeliveryMode: hookenum.DeliveryModeAsync,
		SafeParts:    []string{"run_context", "tool_context", "safe_summary", "correlation_id"},
	})

	result, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if err != nil {
		t.Fatalf("SubmitHookEvent(): %v", err)
	}
	if len(providerRoute.Events()) != 0 {
		t.Fatalf("provider route received %d events, want 0", len(providerRoute.Events()))
	}
	if !hasDiagnosticCode(result.RouteDiagnostics, hookenum.DownstreamOwnerProviderHub, value.RouteDiagnosticUnexpected) {
		t.Fatalf("route diagnostics = %+v, want unexpected provider route", result.RouteDiagnostics)
	}
}

func TestSubmitHookEventDoesNotReplayDispatchForDuplicate(t *testing.T) {
	t.Parallel()

	route := &recordingOwnerRoute{}
	service := newTestServiceWithConfig(Config{}, testRouteRegistry(map[hookenum.DownstreamOwner]OwnerRoute{
		hookenum.DownstreamOwnerOperationsFeed: route,
	}))
	envelope := validPreToolUseEnvelope()
	if _, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope}); err != nil {
		t.Fatalf("first SubmitHookEvent(): %v", err)
	}
	result, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if err != nil {
		t.Fatalf("second SubmitHookEvent(): %v", err)
	}
	if !result.Duplicate {
		t.Fatal("duplicate = false, want true")
	}
	if result.RoutesAccepted != 4 {
		t.Fatalf("duplicate routes accepted = %d, want cached 4", result.RoutesAccepted)
	}
	if len(route.Events()) != 1 {
		t.Fatalf("route dispatch count = %d, want 1", len(route.Events()))
	}
}

func TestSubmitHookEventRetriesIncompleteDuplicateDelivery(t *testing.T) {
	t.Parallel()

	route := &recordingOwnerRoute{}
	repository := &failOnceDeliveryRepository{inner: hookstub.NewRepository()}
	service := New(repository, Config{}, Dependencies{
		Clock: fixedClock{now: time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)},
		RouteRegistry: testRouteRegistry(map[hookenum.DownstreamOwner]OwnerRoute{
			hookenum.DownstreamOwnerOperationsFeed: route,
		}),
		RateLimiter: NewFixedWindowRateLimiter(RateLimitConfig{Window: time.Minute, Burst: 1}),
	})
	envelope := validPreToolUseEnvelope()

	_, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if err == nil {
		t.Fatal("first SubmitHookEvent() error is nil, want delivery persistence error")
	}
	result, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if err != nil {
		t.Fatalf("second SubmitHookEvent(): %v", err)
	}
	if !result.Duplicate {
		t.Fatal("duplicate = false, want true for retry after incomplete delivery")
	}
	if result.RoutesAccepted != 4 {
		t.Fatalf("routes accepted = %d, want 4 after retry", result.RoutesAccepted)
	}
	if len(route.Events()) != 2 {
		t.Fatalf("route dispatch count = %d, want retry dispatch", len(route.Events()))
	}
}

func TestSubmitHookEventCanFailClosedOnRouteFailure(t *testing.T) {
	t.Parallel()

	service := newTestServiceWithConfig(
		Config{
			DisabledRoutes:     []hookenum.DownstreamOwner{hookenum.DownstreamOwnerOperationsFeed},
			RouteFailurePolicy: hookenum.RouteFailurePolicyFailClosed,
		},
		NewDefaultRouteRegistry(),
	)

	result, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: validPreToolUseEnvelope()})
	if err != nil {
		t.Fatalf("SubmitHookEvent(): %v", err)
	}
	if result.HandlerResult.Result != hookenum.HandlerResultFailClosed {
		t.Fatalf("handler result = %s, want fail_closed", result.HandlerResult.Result)
	}
	if result.HandlerResult.DecisionReason != value.RouteDiagnosticFailurePolicyFired {
		t.Fatalf("decision reason = %q, want route failure policy", result.HandlerResult.DecisionReason)
	}
}

func TestSubmitHookEventRecordsSafeOpsFeedAndDiagnosticsMetrics(t *testing.T) {
	t.Parallel()

	opsFeed := opsstub.NewRepository(opsstub.Config{Capacity: 8, Retention: 365 * 24 * time.Hour})
	service := newTestServiceWithDependencies(
		Config{
			DisabledRoutes:   []hookenum.DownstreamOwner{hookenum.DownstreamOwnerRuntimeManager},
			OpsFeedRetention: 365 * 24 * time.Hour,
		},
		NewRouteRegistry(map[hookenum.DownstreamOwner]OwnerRoute{
			hookenum.DownstreamOwnerAgentManager:   NoopOwnerRoute{Owner: hookenum.DownstreamOwnerAgentManager},
			hookenum.DownstreamOwnerOperationsFeed: failingOwnerRoute{err: errors.New("raw stdout secret detail")},
		}),
		opsFeed,
		NewFixedWindowRateLimiter(RateLimitConfig{Window: time.Minute, Burst: 10}),
	)
	envelope := validPreToolUseEnvelope()
	envelope.SanitizerReport.Result = hookenum.SanitizerResultRedacted
	envelope.SanitizerReport.RedactionCount = 2

	result, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if err != nil {
		t.Fatalf("SubmitHookEvent(): %v", err)
	}
	if result.RoutesAccepted != 1 {
		t.Fatalf("routes accepted = %d, want 1", result.RoutesAccepted)
	}
	snapshot := service.OpsDiagnosticsSnapshot(context.Background())
	if snapshot.Accepted != 1 || snapshot.Redacted != 1 || snapshot.Disabled != 1 || snapshot.Unsupported != 1 || snapshot.DownstreamFailed != 1 {
		t.Fatalf("ops snapshot = %+v, want accepted/redacted/disabled/unsupported/downstream_failed", snapshot)
	}
	entries := service.RecentOpsFeed(context.Background(), 1)
	if len(entries) != 1 {
		t.Fatalf("ops feed entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.SafeSummary == "" || entry.PayloadDigest != envelope.PayloadDigest || entry.PayloadSizeBucket == "" || entry.LatencyBucket == "" {
		t.Fatalf("ops entry is missing safe summary/digest/buckets: %+v", entry)
	}
	if strings.Contains(entry.SafeSummary, "raw stdout secret detail") || strings.Contains(entry.RejectReason, "raw stdout secret detail") {
		t.Fatalf("ops entry leaked downstream raw detail: %+v", entry)
	}
}

func TestSubmitHookEventRateLimitDropsBeforeDispatch(t *testing.T) {
	t.Parallel()

	route := &recordingOwnerRoute{}
	service := newTestServiceWithDependencies(
		Config{OpsFeedRetention: 365 * 24 * time.Hour},
		testRouteRegistry(map[hookenum.DownstreamOwner]OwnerRoute{
			hookenum.DownstreamOwnerOperationsFeed: route,
		}),
		opsstub.NewRepository(opsstub.Config{Capacity: 8, Retention: 365 * 24 * time.Hour}),
		NewFixedWindowRateLimiter(RateLimitConfig{Window: time.Minute, Burst: 1}),
	)
	first := validPreToolUseEnvelope()
	second := validPreToolUseEnvelopeWith(uuid.MustParse("11111111-2222-4111-8111-222222222222"), digest("b"), "run-5555:pre-tool-use:toolu-002")

	if _, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: first}); err != nil {
		t.Fatalf("first SubmitHookEvent(): %v", err)
	}
	_, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: second})
	if !errors.Is(err, hookerrs.ErrRateLimited) {
		t.Fatalf("second SubmitHookEvent() error = %v, want ErrRateLimited", err)
	}
	if len(route.Events()) != 1 {
		t.Fatalf("route dispatch count = %d, want only first event", len(route.Events()))
	}
	snapshot := service.OpsDiagnosticsSnapshot(context.Background())
	if snapshot.Dropped != 1 {
		t.Fatalf("dropped metric = %d, want 1", snapshot.Dropped)
	}
}

func TestSubmitHookEventRejectedOpsDiagnosticOmitsUnsafeSummary(t *testing.T) {
	t.Parallel()

	service := newTestServiceWithDependencies(
		Config{OpsFeedRetention: 365 * 24 * time.Hour},
		NewDefaultRouteRegistry(),
		opsstub.NewRepository(opsstub.Config{Capacity: 8, Retention: 365 * 24 * time.Hour}),
		NewFixedWindowRateLimiter(RateLimitConfig{Window: time.Minute, Burst: 10}),
	)
	envelope := validPreToolUseEnvelope()
	envelope.SafePayload.SafeSummary = "unsafe rejected preview must not be copied"
	envelope.SanitizerReport.RejectedFieldClasses = []string{"secret"}

	_, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if !errors.Is(err, hookerrs.ErrPayloadRejected) {
		t.Fatalf("SubmitHookEvent() error = %v, want ErrPayloadRejected", err)
	}
	entries := service.RecentOpsFeed(context.Background(), 1)
	if len(entries) != 1 {
		t.Fatalf("ops feed entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Status != hookenum.OpsFeedStatusRejected || entry.RejectReason != string(hookerrs.ErrPayloadRejected) {
		t.Fatalf("ops entry = %+v, want rejected payload diagnostic", entry)
	}
	if entry.SafeSummary != "" {
		t.Fatalf("rejected ops entry copied unsafe summary: %q", entry.SafeSummary)
	}
}

func TestSubmitHookEventBackpressurePreventsDispatch(t *testing.T) {
	t.Parallel()

	route := &recordingOwnerRoute{}
	service := newTestServiceWithDependencies(
		Config{OpsFeedRetention: 365 * 24 * time.Hour},
		testRouteRegistry(map[hookenum.DownstreamOwner]OwnerRoute{
			hookenum.DownstreamOwnerOperationsFeed: route,
		}),
		opsstub.NewRepository(opsstub.Config{Capacity: 1, Retention: 365 * 24 * time.Hour}),
		NewFixedWindowRateLimiter(RateLimitConfig{Window: time.Minute, Burst: 10}),
	)
	first := validPreToolUseEnvelope()
	second := validPreToolUseEnvelopeWith(uuid.MustParse("11111111-2222-4111-8111-333333333333"), digest("c"), "run-5555:pre-tool-use:toolu-003")

	if _, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: first}); err != nil {
		t.Fatalf("first SubmitHookEvent(): %v", err)
	}
	_, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: second})
	if !errors.Is(err, hookerrs.ErrBackpressure) {
		t.Fatalf("second SubmitHookEvent() error = %v, want ErrBackpressure", err)
	}
	if len(route.Events()) != 1 {
		t.Fatalf("route dispatch count = %d, want backpressure before second dispatch", len(route.Events()))
	}
}

func TestSubmitHookEventDuplicateReplayDoesNotDuplicateOpsFeed(t *testing.T) {
	t.Parallel()

	route := &recordingOwnerRoute{}
	service := newTestServiceWithDependencies(
		Config{OpsFeedRetention: 365 * 24 * time.Hour},
		testRouteRegistry(map[hookenum.DownstreamOwner]OwnerRoute{
			hookenum.DownstreamOwnerOperationsFeed: route,
		}),
		opsstub.NewRepository(opsstub.Config{Capacity: 8, Retention: 365 * 24 * time.Hour}),
		NewFixedWindowRateLimiter(RateLimitConfig{Window: time.Minute, Burst: 1}),
	)
	envelope := validPreToolUseEnvelope()
	if _, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope}); err != nil {
		t.Fatalf("first SubmitHookEvent(): %v", err)
	}
	result, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if err != nil {
		t.Fatalf("second SubmitHookEvent(): %v", err)
	}
	if !result.Duplicate {
		t.Fatal("duplicate = false, want true")
	}
	if len(route.Events()) != 1 {
		t.Fatalf("route dispatch count = %d, want one dispatch", len(route.Events()))
	}
	if entries := service.RecentOpsFeed(context.Background(), 10); len(entries) != 1 {
		t.Fatalf("ops feed entries = %d, want 1", len(entries))
	}
	if snapshot := service.OpsDiagnosticsSnapshot(context.Background()); snapshot.Dropped != 0 {
		t.Fatalf("dropped metric = %d, want duplicate replay to bypass rate limit", snapshot.Dropped)
	}
}

func TestSubmitHookEventReturnsCachedResultForDuplicateDigest(t *testing.T) {
	t.Parallel()

	service := newTestService()
	envelope := validPreToolUseEnvelope()
	if _, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope}); err != nil {
		t.Fatalf("first SubmitHookEvent(): %v", err)
	}
	result, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if err != nil {
		t.Fatalf("second SubmitHookEvent(): %v", err)
	}
	if !result.Duplicate {
		t.Fatal("duplicate = false, want true")
	}
}

func TestSubmitHookEventRejectsDuplicateConflict(t *testing.T) {
	t.Parallel()

	service := newTestService()
	envelope := validPreToolUseEnvelope()
	if _, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope}); err != nil {
		t.Fatalf("first SubmitHookEvent(): %v", err)
	}
	envelope.PayloadDigest = digest("b")
	_, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if !errors.Is(err, hookerrs.ErrDuplicateConflict) {
		t.Fatalf("error = %v, want ErrDuplicateConflict", err)
	}
}

func TestSubmitHookEventDetectsConcurrentDuplicateDigestConflict(t *testing.T) {
	t.Parallel()

	service := newTestService()
	first := validPreToolUseEnvelope()
	second := first
	second.PayloadDigest = digest("b")

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, envelope := range []value.HookEnvelope{first, second} {
		envelope := envelope
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, hookerrs.ErrDuplicateConflict):
			conflicts++
		default:
			t.Fatalf("error = %v, want nil or ErrDuplicateConflict", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}
}

func TestSubmitHookEventRejectsUntrustedSource(t *testing.T) {
	t.Parallel()

	envelope := validPreToolUseEnvelope()
	envelope.SourceContext.TrustLevel = hookenum.TrustLevelUntrustedRejected
	_, err := newTestService().SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if !errors.Is(err, hookerrs.ErrInvalidBinding) {
		t.Fatalf("error = %v, want ErrInvalidBinding", err)
	}
}

func TestSubmitHookEventRejectsOversizedSafeSummary(t *testing.T) {
	t.Parallel()

	envelope := validPreToolUseEnvelope()
	envelope.SafePayload.SafeSummary = strings.Repeat("\u0100", 4096)
	_, err := newTestService().SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if !errors.Is(err, hookerrs.ErrPayloadRejected) {
		t.Fatalf("error = %v, want ErrPayloadRejected", err)
	}
}

func TestSubmitHookEventRejectsSanitizerRejectedClasses(t *testing.T) {
	t.Parallel()

	envelope := validPreToolUseEnvelope()
	envelope.SanitizerReport.RejectedFieldClasses = []string{"secret"}
	_, err := newTestService().SubmitHookEvent(context.Background(), SubmitHookEventInput{Envelope: envelope})
	if !errors.Is(err, hookerrs.ErrPayloadRejected) {
		t.Fatalf("error = %v, want ErrPayloadRejected", err)
	}
}

func TestDefaultEnvelopeValidatorRejectsSchemaBoundViolations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*value.HookEnvelope)
	}{
		{
			name: "retention class enum",
			mutate: func(envelope *value.HookEnvelope) {
				envelope.RetentionClass = "forever"
			},
		},
		{
			name: "route owner enum",
			mutate: func(envelope *value.HookEnvelope) {
				envelope.DownstreamRoutes[0].Owner = "raw-store"
			},
		},
		{
			name: "safe part allowlist",
			mutate: func(envelope *value.HookEnvelope) {
				envelope.DownstreamRoutes[0].SafeParts = []string{"raw_prompt"}
			},
		},
		{
			name: "tool category enum",
			mutate: func(envelope *value.HookEnvelope) {
				envelope.ToolContext.ToolCategory = "network"
			},
		},
		{
			name: "risk class enum",
			mutate: func(envelope *value.HookEnvelope) {
				envelope.SafePayload.RiskClass = "critical"
			},
		},
		{
			name: "timeout budget range",
			mutate: func(envelope *value.HookEnvelope) {
				timeout := 0
				envelope.SafePayload.TimeoutBudgetMS = &timeout
			},
		},
		{
			name: "exit status range",
			mutate: func(envelope *value.HookEnvelope) {
				status := 256
				envelope.SafePayload.ExitStatus = &status
			},
		},
		{
			name: "correlation id pattern",
			mutate: func(envelope *value.HookEnvelope) {
				envelope.CorrelationID = "run 5555"
			},
		},
		{
			name: "prompt digest format",
			mutate: func(envelope *value.HookEnvelope) {
				envelope.SafePayload.PromptDigest = "sha256:short"
			},
		},
	}

	validator := DefaultEnvelopeValidator{}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelope := validPreToolUseEnvelope()
			tc.mutate(&envelope)
			err := validator.ValidateEnvelope(context.Background(), normalizedConfig(Config{}), envelope)
			if !errors.Is(err, hookerrs.ErrInvalidArgument) {
				t.Fatalf("ValidateEnvelope() error = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func newTestService() *Service {
	return newTestServiceWithConfig(Config{}, NewDefaultRouteRegistry())
}

func newTestServiceWithConfig(cfg Config, registry *RouteRegistry) *Service {
	return New(hookstub.NewRepository(), cfg, Dependencies{
		Clock:         fixedClock{now: time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)},
		RouteRegistry: registry,
	})
}

func newTestServiceWithDependencies(
	cfg Config,
	registry *RouteRegistry,
	opsFeed *opsstub.Repository,
	rateLimiter RateLimiter,
) *Service {
	return New(hookstub.NewRepository(), cfg, Dependencies{
		Clock:         fixedClock{now: time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)},
		RouteRegistry: registry,
		OpsFeed:       opsFeed,
		RateLimiter:   rateLimiter,
	})
}

func testRouteRegistry(overrides map[hookenum.DownstreamOwner]OwnerRoute) *RouteRegistry {
	dispatchers := make(map[hookenum.DownstreamOwner]OwnerRoute, len(hookenum.DownstreamOwners()))
	for _, owner := range hookenum.DownstreamOwners() {
		dispatchers[owner] = NoopOwnerRoute{Owner: owner}
	}
	for owner, route := range overrides {
		dispatchers[owner] = route
	}
	return NewRouteRegistry(dispatchers)
}

func validPreToolUseEnvelope() value.HookEnvelope {
	turnID := "turn-12"
	return value.HookEnvelope{
		EventID:       uuid.MustParse("11111111-2222-4111-8111-111111111111"),
		SchemaVersion: "codex-hook-ingress.normalized-hook-envelope.v1",
		HookEventName: hookenum.HookEventPreToolUse,
		EventTime:     time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC),
		SourceContext: value.SourceContext{
			SourceRef:      "hook-emitter:slot-7",
			SourceKind:     hookenum.SourceKindHookEmitter,
			ActorRef:       "agent-manager:run-worker",
			OrganizationID: uuid.MustParse("22222222-2222-4222-8222-222222222222"),
			ProjectID:      uuid.MustParse("33333333-3333-4333-8333-333333333333"),
			EmitterVersion: "0.1.0",
			TrustLevel:     hookenum.TrustLevelManaged,
		},
		RunContext: value.RunContext{
			RunID:     uuid.MustParse("55555555-5555-4555-8555-555555555555"),
			SessionID: "codex-session-7",
			SlotID:    uuid.MustParse("66666666-6666-4666-8666-666666666666"),
			TurnID:    &turnID,
		},
		ToolContext: &value.ToolContext{
			ToolName:     "Bash",
			ToolCategory: hookenum.ToolCategoryShell,
			ToolUseID:    "toolu-001",
		},
		SafePayload: value.SafePayload{
			SafeSummary: "Agent intends to run a documentation check command.",
			RiskClass:   "low",
		},
		PayloadDigest: digest("a"),
		SanitizerReport: value.SanitizerReport{
			Result:         hookenum.SanitizerResultAccepted,
			AppliedRules:   []string{"hash-command"},
			RedactionCount: 0,
		},
		DownstreamRoutes: []value.DownstreamRoute{
			{
				Owner:        hookenum.DownstreamOwnerOperationsFeed,
				DeliveryMode: hookenum.DeliveryModeRealtime,
				SafeParts:    []string{"run_context", "tool_context", "safe_summary", "correlation_id"},
			},
		},
		CorrelationID:  "run-5555:pre-tool-use:toolu-001",
		RetentionClass: hookenum.RetentionClassRealtime,
	}
}

func validPreToolUseEnvelopeWith(eventID uuid.UUID, payloadDigest string, correlationID string) value.HookEnvelope {
	envelope := validPreToolUseEnvelope()
	envelope.EventID = eventID
	envelope.PayloadDigest = payloadDigest
	envelope.CorrelationID = correlationID
	envelope.ToolContext.ToolUseID = correlationID[strings.LastIndex(correlationID, ":")+1:]
	return envelope
}

func digest(char string) string {
	return "sha256:" + strings.Repeat(char, 64)
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type recordingOwnerRoute struct {
	mu     sync.Mutex
	events []value.SafeHookEvent
}

func (route *recordingOwnerRoute) DispatchSafeHookEvent(_ context.Context, event value.SafeHookEvent) error {
	route.mu.Lock()
	defer route.mu.Unlock()
	route.events = append(route.events, event)
	return nil
}

func (route *recordingOwnerRoute) Events() []value.SafeHookEvent {
	route.mu.Lock()
	defer route.mu.Unlock()
	return append([]value.SafeHookEvent(nil), route.events...)
}

type failingOwnerRoute struct {
	err error
}

func (route failingOwnerRoute) DispatchSafeHookEvent(_ context.Context, _ value.SafeHookEvent) error {
	return route.err
}

type failOnceDeliveryRepository struct {
	inner  *hookstub.Repository
	mu     sync.Mutex
	failed bool
}

func (repository *failOnceDeliveryRepository) Ready() bool {
	return repository != nil && repository.inner != nil && repository.inner.Ready()
}

func (repository *failOnceDeliveryRepository) RegisterAcceptedEvent(ctx context.Context, event entity.AcceptedEvent) (entity.AcceptedEvent, bool, error) {
	return repository.inner.RegisterAcceptedEvent(ctx, event)
}

func (repository *failOnceDeliveryRepository) FindAcceptedEvent(ctx context.Context, eventID uuid.UUID) (entity.AcceptedEvent, bool, error) {
	return repository.inner.FindAcceptedEvent(ctx, eventID)
}

func (repository *failOnceDeliveryRepository) RecordDeliveryResults(ctx context.Context, update entity.DeliveryUpdate) (entity.AcceptedEvent, error) {
	repository.mu.Lock()
	if !repository.failed {
		repository.failed = true
		repository.mu.Unlock()
		return entity.AcceptedEvent{}, errors.New("delivery diagnostics persistence failed")
	}
	repository.mu.Unlock()
	return repository.inner.RecordDeliveryResults(ctx, update)
}

func hasRouteStatus(diagnostics []value.RouteDeliveryResult, owner hookenum.DownstreamOwner, status hookenum.RouteDeliveryStatus) bool {
	return diagnosticForOwner(diagnostics, owner).Status == status
}

func hasDiagnosticCode(diagnostics []value.RouteDeliveryResult, owner hookenum.DownstreamOwner, code string) bool {
	return diagnosticForOwner(diagnostics, owner).DiagnosticCode == code
}

func diagnosticForOwner(diagnostics []value.RouteDeliveryResult, owner hookenum.DownstreamOwner) value.RouteDeliveryResult {
	for _, diagnostic := range diagnostics {
		if diagnostic.Owner == owner {
			return diagnostic
		}
	}
	return value.RouteDeliveryResult{}
}
