package transcription

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/audio/ffmpeg"
)

type boundedProvider struct {
	deadline time.Time
	calls    int
}

func (provider *boundedProvider) Transcribe(ctx context.Context, _ value.ProviderRequest) (string, error) {
	provider.deadline, _ = ctx.Deadline()
	provider.calls++
	return "synthetic result", nil
}

func (provider *boundedProvider) CheckModel(ctx context.Context, _ string, _ []byte) error {
	provider.deadline, _ = ctx.Deadline()
	provider.calls++
	return nil
}

func (*boundedProvider) CheckLocal(context.Context) error  { return nil }
func (*boundedProvider) CheckEgress(context.Context) error { return nil }

func TestSiblingContinuationsUseEarliestExpiry(t *testing.T) {
	for _, probe := range []bool{false, true} {
		for _, policyFirst := range []bool{false, true} {
			now := time.Now().UTC()
			policy := validPolicy(now)
			credential := validCredential(policy, []byte("synthetic-key"), now)
			policy.ExpiresAt = now.Add(2 * time.Second)
			credential.ExpiresAt = now.Add(4 * time.Second)
			if !policyFirst {
				policy.ExpiresAt, credential.ExpiresAt = credential.ExpiresAt, policy.ExpiresAt
			}
			provider := &boundedProvider{}
			service, err := New(&fakePolicy{policy: policy}, &fakeCredential{credential: credential}, provider, &observed{}, 10*time.Second, ffmpeg.New(t.TempDir()))
			if err != nil {
				t.Fatal(err)
			}
			service.now = func() time.Time { return now }
			input := validInput(now, pcmWAV(time.Second))
			if probe {
				availability, err := service.CheckAvailability(t.Context(), input.Principal, input.CorrelationID)
				if err != nil || !availability.ValidUntil.Equal(now.Add(2*time.Second)) {
					t.Fatalf("bounded availability failed: %v", err)
				}
			} else if _, err := service.Transcribe(t.Context(), input); err != nil {
				t.Fatalf("valid sibling continuations rejected: %v", err)
			}
			if provider.calls != 1 || provider.deadline.IsZero() || provider.deadline.After(now.Add(2*time.Second)) {
				t.Fatal("provider call escaped earliest continuation expiry")
			}
		}
	}
}

func TestExpiredSiblingContinuationStopsBeforeProvider(t *testing.T) {
	for _, policyExpired := range []bool{false, true} {
		now := time.Now().UTC()
		policy := validPolicy(now)
		credential := validCredential(policy, []byte("synthetic-key"), now)
		if policyExpired {
			policy.ExpiresAt = now
		} else {
			credential.ExpiresAt = now
		}
		provider := &boundedProvider{}
		service, err := New(&fakePolicy{policy: policy}, &fakeCredential{credential: credential}, provider, &observed{}, 10*time.Second, ffmpeg.New(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		service.now = func() time.Time { return now }
		input := validInput(now, pcmWAV(time.Second))
		if _, err := service.CheckAvailability(t.Context(), input.Principal, input.CorrelationID); err == nil || provider.calls != 0 {
			t.Fatal("expired continuation reached provider probe")
		}
	}
}
