package grpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTransportErrorDoesNotExposeProviderDiagnostics(t *testing.T) {
	mapped := transportError(errors.Join(errs.ErrProviderUnavailable, errors.New("sensitive provider body")))
	if status.Code(mapped) != codes.Unavailable || strings.Contains(mapped.Error(), "sensitive provider body") {
		t.Fatalf("небезопасная transport-ошибка: %v", mapped)
	}
}

func TestTransportErrorPreservesRequestCancellation(t *testing.T) {
	tests := []struct {
		err  error
		code codes.Code
	}{
		{err: errors.Join(errs.ErrProviderUnavailable, context.Canceled), code: codes.Canceled},
		{err: errors.Join(errs.ErrProviderUnavailable, context.DeadlineExceeded), code: codes.DeadlineExceeded},
	}
	for _, test := range tests {
		if actual := status.Code(transportError(test.err)); actual != test.code {
			t.Fatalf("код отмены %s, ожидался %s", actual, test.code)
		}
	}
}
