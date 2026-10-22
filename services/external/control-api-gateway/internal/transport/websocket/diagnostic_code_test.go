package websockettransport

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestSnapshotDiagnosticGRPCCodePreservesClosedRange independently enumerates every canonical status and rejects out-of-range values including the maximum unsigned code, retaining nil, ordinary and wrapped-error mappings without logging their content.
// TestSnapshotDiagnosticGRPCCodePreservesClosedRange независимо перечисляет все канонические статусы и отклоняет значения вне диапазона, включая максимальный беззнаковый код, сохраняя отображения nil, обычной и обёрнутой ошибки без записи их содержимого.
func TestSnapshotDiagnosticGRPCCodePreservesClosedRange(t *testing.T) {
	t.Parallel()
	for code := codes.OK; code <= codes.Unauthenticated; code++ {
		t.Run(code.String(), func(t *testing.T) {
			if got := snapshotDiagnosticGRPCCode(status.Error(code, "private diagnostic input")); got != code {
				t.Fatalf("canonical diagnostic code = %v, want %v", got, code)
			}
		})
	}
	for _, test := range []struct {
		name string
		err  error
		want codes.Code
	}{
		{name: "nil", want: codes.OK},
		{name: "ordinary", err: errors.New("private diagnostic input"), want: codes.Unknown},
		{name: "wrapped", err: fmt.Errorf("private wrapper: %w", status.Error(codes.DeadlineExceeded, "private diagnostic input")), want: codes.DeadlineExceeded},
		{name: "maximum unsigned", err: status.Error(codes.Code(^uint32(0)), "private diagnostic input"), want: codes.Unknown},
		{name: "out of range", err: status.Error(codes.Code(999), "private diagnostic input"), want: codes.Unknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := snapshotDiagnosticGRPCCode(test.err); got != test.want {
				t.Fatalf("diagnostic code = %v, want %v", got, test.want)
			}
		})
	}
}
