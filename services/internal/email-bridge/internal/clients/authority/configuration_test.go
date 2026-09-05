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

type configurationReadback struct {
	cp.RuntimeWorkServiceClient
	call func(context.Context, *cp.ReportEmailConfigurationReadbackRequest) (*cp.ReportEmailConfigurationReadbackResponse, error)
}

func (f configurationReadback) ReportEmailConfigurationReadback(ctx context.Context, request *cp.ReportEmailConfigurationReadbackRequest, _ ...grpc.CallOption) (*cp.ReportEmailConfigurationReadbackResponse, error) {
	return f.call(ctx, request)
}

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
