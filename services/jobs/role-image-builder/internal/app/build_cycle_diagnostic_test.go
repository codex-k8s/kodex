package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestBuildCycleRPCCode trusts typed errors rather than message text and constrains unknown
// numeric codes. Context fallback and joined/wrapped RPC codes preserve a bounded diagnostic.
// TestBuildCycleRPCCode доверяет typed errors, не тексту, и ограничивает неизвестные numeric
// codes. Context fallback и joined/wrapped RPC codes сохраняют bounded diagnostic.
func TestBuildCycleRPCCode(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want codes.Code
	}{
		{"wrapped", fmt.Errorf("synthetic-sensitive-detail: %w", status.Error(codes.PermissionDenied, "synthetic-sensitive-detail")), codes.PermissionDenied},
		{"joined", errors.Join(errors.New("synthetic-sensitive-detail"), status.Error(codes.FailedPrecondition, "synthetic-sensitive-detail")), codes.FailedPrecondition},
		{"text", errors.New("rpc error: code = PermissionDenied desc = synthetic-sensitive-detail"), codes.Unknown},
		{"unknown-number", status.Error(codes.Code(5000), "synthetic-sensitive-detail"), codes.Unknown},
		{"deadline", fmt.Errorf("synthetic-sensitive-detail: %w", context.DeadlineExceeded), codes.DeadlineExceeded},
		{"cancelled", errors.Join(errors.New("synthetic-sensitive-detail"), context.Canceled), codes.Canceled},
		{"nil", nil, codes.Unknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := buildCycleRPCCode(test.err); got != test.want {
				t.Fatalf("closed code mismatch: got=%s want=%s", got, test.want)
			}
		})
	}
	for code := codes.Canceled; code <= codes.Unauthenticated; code++ {
		if got := buildCycleRPCCode(status.Error(code, "synthetic-sensitive-detail")); got != code {
			t.Fatal("canonical code lost")
		}
	}
}

// TestBuildCycleDiagnosticDoesNotExportErrorDetails checks the actual log and telemetry sink
// receive only the fixed transition and closed code, without retaining the source error chain.
// TestBuildCycleDiagnosticDoesNotExportErrorDetails проверяет фактические log и telemetry sink:
// лишь постоянный переход и закрытый code, без исходной error chain.
func TestBuildCycleDiagnosticDoesNotExportErrorDetails(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	original := status.Error(codes.PermissionDenied, "synthetic-sensitive-detail")
	calls := 0
	reportBuildCycleDegraded(t.Context(), logger, func(_ context.Context, err error) {
		calls++
		if strings.Contains(err.Error(), "synthetic-sensitive-detail") || errors.Is(err, original) || errors.Unwrap(err) != nil {
			t.Error("telemetry retained private detail or source chain")
		}
		if err.Error() != buildCycleDegradedMessage+": PermissionDenied" {
			t.Error("telemetry projection mismatch")
		}
	}, original)
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal("diagnostic log is not JSON")
	}
	if calls != 1 || record["msg"] != buildCycleDegradedMessage || record["rpc_code"] != "PermissionDenied" || record["error_class"] != "control_plane_or_buildkit" || strings.Contains(output.String(), "synthetic-sensitive-detail") {
		t.Fatal("diagnostic projection is not closed")
	}
	if len(record) != 5 {
		t.Fatal("unexpected diagnostic fields")
	}
}
