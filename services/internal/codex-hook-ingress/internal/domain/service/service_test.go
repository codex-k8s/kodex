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
	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
	hookstub "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/repository/stub/hook"
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
	if result.RoutesAccepted != 1 {
		t.Fatalf("routes accepted = %d, want 1", result.RoutesAccepted)
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
	return New(hookstub.NewRepository(), Config{}, Dependencies{Clock: fixedClock{now: time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)}})
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

func digest(char string) string {
	return "sha256:" + strings.Repeat(char, 64)
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}
