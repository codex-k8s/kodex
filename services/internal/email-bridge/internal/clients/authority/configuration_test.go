package authority

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"google.golang.org/grpc"
)

// configurationReadback substitutes only the owner configuration-acknowledgement RPC; other embedded client methods remain unused.
// configurationReadback подставляет только owner RPC подтверждения конфигурации; остальные встроенные методы клиента не используются.
type configurationReadback struct {
	cp.RuntimeWorkServiceClient
	call func(context.Context, *cp.ReportEmailConfigurationReadbackRequest) (*cp.ReportEmailConfigurationReadbackResponse, error)
}

// ReportEmailConfigurationReadback forwards the exact context and revision/digest request to the test callback without retries.
// ReportEmailConfigurationReadback передаёт точные контекст и запрос revision/digest тестовому callback без повторов.
func (f configurationReadback) ReportEmailConfigurationReadback(ctx context.Context, request *cp.ReportEmailConfigurationReadbackRequest, _ ...grpc.CallOption) (*cp.ReportEmailConfigurationReadbackResponse, error) {
	return f.call(ctx, request)
}

// TestConfigurationReadbackExactBoundedAcknowledgement checks three-second request bounds, exact pins, cancellation, accepted status and safe failure mapping.
// Invalid local pins and missing clients must fail before the synthetic owner callback is reached.
// TestConfigurationReadbackExactBoundedAcknowledgement проверяет трёхсекундный бюджет, точные привязки, отмену, accepted и безопасные ошибки.
// Неверные локальные привязки и отсутствие клиента должны отклоняться до синтетического callback владельца.
func TestConfigurationReadbackExactBoundedAcknowledgement(t *testing.T) {
	for _, outcome := range []string{"accepted", "rejected", "missing", "failure", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			calls := 0
			client := &Client{API: configurationReadback{call: func(ctx context.Context, request *cp.ReportEmailConfigurationReadbackRequest) (*cp.ReportEmailConfigurationReadbackResponse, error) {
				calls++
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 3*time.Second || request.Revision != 42 || request.Digest != strings.Repeat("a", 64) {
					t.Fatal("configuration readback lost exact pin or deadline")
				}
				switch outcome {
				case "missing":
					return nil, nil
				case "failure":
					return nil, errors.New("private upstream detail")
				case "cancelled":
					return nil, ctx.Err()
				default:
					return &cp.ReportEmailConfigurationReadbackResponse{Accepted: outcome == "accepted"}, nil
				}
			}}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if outcome == "cancelled" {
				cancel()
			}
			err := client.ReportConfigurationReadback(ctx, 42, strings.Repeat("a", 64))
			if calls != 1 || (err == nil) != (outcome == "accepted") || err != nil && !errors.Is(err, errs.Unavailable) {
				t.Fatal("configuration readback did not fail closed without retry")
			}
		})
	}
	client := &Client{API: configurationReadback{call: func(context.Context, *cp.ReportEmailConfigurationReadbackRequest) (*cp.ReportEmailConfigurationReadbackResponse, error) {
		t.Fatal("invalid local pin reached owner")
		return nil, nil
	}}}
	for _, digest := range []string{"", "wrong", strings.Repeat("A", 64), strings.Repeat("a", 63), strings.Repeat("g", 64)} {
		if client.ReportConfigurationReadback(t.Context(), 42, digest) == nil {
			t.Fatal("invalid digest accepted")
		}
	}
	if client.ReportConfigurationReadback(t.Context(), 0, strings.Repeat("a", 64)) == nil || (&Client{}).ReportConfigurationReadback(t.Context(), 42, strings.Repeat("a", 64)) == nil {
		t.Fatal("missing owner or revision accepted")
	}
}
