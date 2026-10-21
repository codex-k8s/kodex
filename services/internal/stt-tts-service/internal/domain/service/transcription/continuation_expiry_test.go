package transcription

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/audio/ffmpeg"
)

// boundedProvider records the actual model/probe deadline and call count
// so expiry tests can detect an escaped or repeated external effect.
// RU: boundedProvider фиксирует срок и число вызовов модели или проверки,
// чтобы тесты обнаруживали выход за срок либо повтор внешнего действия.
type boundedProvider struct {
	deadline time.Time
	calls    int
}

// Transcribe records the effective deadline of the synthetic recognition call.
// Transcribe фиксирует действующий срок тестового вызова распознавания.
func (provider *boundedProvider) Transcribe(ctx context.Context, _ value.ProviderRequest) (string, error) {
	provider.deadline, _ = ctx.Deadline()
	provider.calls++
	return "synthetic result", nil
}

// CheckModel records the same deadline for the non-billable availability probe.
// CheckModel фиксирует тот же срок для проверки доступности без распознавания.
func (provider *boundedProvider) CheckModel(ctx context.Context, _ string, _ []byte) error {
	provider.deadline, _ = ctx.Deadline()
	provider.calls++
	return nil
}

// CheckLocal keeps local prerequisites available in the expiry fixture.
// CheckLocal оставляет локальные зависимости доступными в тесте сроков.
func (*boundedProvider) CheckLocal(context.Context) error { return nil }

// CheckEgress keeps the restricted route available in the expiry fixture.
// CheckEgress оставляет ограниченный маршрут доступным в тесте сроков.
func (*boundedProvider) CheckEgress(context.Context) error { return nil }

// TestSiblingContinuationsUseEarliestExpiry checks both call paths and both
// policy/credential expiry orders against the earliest effective deadline.
// RU: TestSiblingContinuationsUseEarliestExpiry проверяет оба пути и оба порядка
// истечения политики и реквизита по ближайшему действующему сроку.
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

// TestExpiredSiblingContinuationStopsBeforeProvider ensures either expired
// sibling projection blocks the probe before the provider is contacted.
// RU: TestExpiredSiblingContinuationStopsBeforeProvider проверяет, что истечение
// любой связанной проекции останавливает проверку до обращения к провайдеру.
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
