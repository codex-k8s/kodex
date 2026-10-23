package readback

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestReadbackRPCRetryPreservesErrorBoundaries checks both production stage
// prefixes across success, bounded/wrapped Unavailable retries, terminal errors
// and cancellation. Real timers are used, but calls are local callbacks without
// network or credentials; a pre-cancelled first call still runs as before.
// TestReadbackRPCRetryPreservesErrorBoundaries проверяет оба рабочих префикса
// этапов при успехе, ограниченных повторах обёрнутого Unavailable, окончательных
// ошибках и отмене. Таймеры реальные, но вызовы — локальные callbacks без сети
// и credentials; первая попытка с уже отменённым контекстом выполняется как прежде.
func TestReadbackRPCRetryPreservesErrorBoundaries(t *testing.T) {
	t.Parallel()
	unavailable := status.Error(codes.Unavailable, "fixture unavailable")
	denied := status.Error(codes.PermissionDenied, "fixture denied")
	plain := errors.New("fixture plain error")
	for _, operation := range []struct {
		name   string
		prefix string
	}{
		{"challenge", "issue readback attestation challenge"},
		{"submission", "attest served authority state"},
	} {
		for _, testCase := range []struct {
			name         string
			failures     []error
			cancelBefore bool
			cancelInCall bool
			expired      bool
			wantCalls    int
			wantCause    error
			wantDirect   bool
		}{
			{name: "first success", failures: []error{nil}, wantCalls: 1},
			{name: "wrapped unavailable", failures: []error{fmt.Errorf("fixture wrapper: %w", unavailable), nil}, wantCalls: 2},
			{name: "third success", failures: []error{unavailable, unavailable, nil}, wantCalls: 3},
			{name: "retry limit", failures: []error{unavailable, unavailable, unavailable}, wantCalls: 3, wantCause: unavailable},
			{name: "permission denied", failures: []error{denied}, wantCalls: 1, wantCause: denied},
			{name: "plain failure", failures: []error{plain}, wantCalls: 1, wantCause: plain},
			{name: "RPC cancellation", failures: []error{context.Canceled}, wantCalls: 1, wantCause: context.Canceled},
			{name: "wait cancellation", failures: []error{unavailable}, cancelInCall: true, wantCalls: 1, wantCause: context.Canceled, wantDirect: true},
			{name: "wait deadline", failures: []error{unavailable}, expired: true, wantCalls: 1, wantCause: context.DeadlineExceeded, wantDirect: true},
			{name: "cancelled first success", failures: []error{nil}, cancelBefore: true, wantCalls: 1},
		} {
			t.Run(operation.name+"_"+testCase.name, func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithCancel(context.Background())
				if testCase.expired {
					cancel()
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				}
				defer cancel()
				if testCase.cancelBefore {
					cancel()
				}
				fixture := new(int)
				*fixture = 42
				calls := 0
				response, err := callReadbackRPC(ctx, operation.prefix, func() (*int, error) {
					if calls >= len(testCase.failures) {
						t.Fatal("retry exceeded the scripted call count")
					}
					failure := testCase.failures[calls]
					calls++
					if testCase.cancelInCall {
						cancel()
					}
					return fixture, failure
				})
				if calls != testCase.wantCalls {
					t.Fatalf("RPC calls = %d, want %d", calls, testCase.wantCalls)
				}
				if testCase.wantDirect {
					if err != testCase.wantCause || response != nil {
						t.Fatalf("wait cancellation changed direct error or zero response: %v", err)
					}
					return
				}
				if response != fixture || *fixture != 42 {
					t.Fatal("retry changed the callback result identity")
				}
				if testCase.wantCause == nil {
					if err != nil {
						t.Fatalf("successful RPC returned error: %v", err)
					}
					return
				}
				if err == testCase.wantCause || !errors.Is(err, testCase.wantCause) ||
					err.Error() != operation.prefix+": "+testCase.wantCause.Error() ||
					status.Code(err) != status.Code(testCase.wantCause) {
					t.Fatalf("RPC error wrapping changed: %v", err)
				}
			})
		}
	}
}
