package grpc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/sttapi/errorprofile"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTransportErrorDoesNotExposeProviderDiagnostics(t *testing.T) {
	mapped := transportError(errors.Join(errs.ErrProviderUnavailable, errors.New("sensitive provider body")))
	if status.Code(mapped) != codes.Unavailable || strings.Contains(mapped.Error(), "sensitive provider body") {
		t.Fatalf("небезопасная transport-ошибка: %v", mapped)
	}
}

func TestTransportRateLimitOnlyExposesBoundedHint(t *testing.T) {
	for _, delay := range []time.Duration{0, 17 * time.Second, 301 * time.Second, time.Millisecond, -time.Second} {
		mapped := transportError(errors.Join(&errs.ProviderRateLimit{RetryAfter: delay}, errors.New("private provider body")))
		if status.Code(mapped) != codes.ResourceExhausted || strings.Contains(mapped.Error(), "private") {
			t.Fatal("unsafe rate limit status")
		}
		reasonCount, hintCount := 0, 0
		for _, detail := range status.Convert(mapped).Details() {
			switch detail := detail.(type) {
			case *errdetails.ErrorInfo:
				if detail.Domain != errorprofile.Domain || detail.Reason != errorprofile.TranscriptionRateLimited || len(detail.Metadata) != 0 {
					t.Fatal("unsafe rate limit reason")
				}
				reasonCount++
			case *errdetails.RetryInfo:
				if detail.RetryDelay.AsDuration() != 17*time.Second {
					t.Fatal("invalid retry hint escaped")
				}
				hintCount++
			}
		}
		if reasonCount != 1 || (hintCount == 1) != (delay == 17*time.Second) {
			t.Fatal("rate limit details are missing or duplicated")
		}
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
