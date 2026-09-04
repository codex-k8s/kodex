package transcription

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

type fakePolicy struct {
	value value.Policy
	err   error
	calls int
}

func (fake *fakePolicy) Resolve(context.Context, value.Principal, string) (value.Policy, error) {
	fake.calls++
	return fake.value, fake.err
}
func (fake *fakePolicy) Check(context.Context) error { return fake.err }

type fakeCredential struct {
	value value.Credential
	err   error
	calls int
}

func (fake *fakeCredential) Project(context.Context, value.Principal, string, value.Policy) (value.Credential, error) {
	fake.calls++
	return fake.value, fake.err
}
func (fake *fakeCredential) Check(context.Context) error { return fake.err }

type fakeProvider struct {
	text    string
	err     error
	calls   int
	request value.ProviderRequest
}

func (fake *fakeProvider) Transcribe(_ context.Context, request value.ProviderRequest) (string, error) {
	fake.calls++
	fake.request = request
	return fake.text, fake.err
}
func (fake *fakeProvider) Check(context.Context) error { return fake.err }

func TestTranscribeCompletePath(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	policy := validPolicy(now)
	credentialBytes := []byte("test-only-credential")
	policies := &fakePolicy{value: policy}
	credentials := &fakeCredential{value: value.Credential{APIKey: credentialBytes, ProviderAccountRef: policy.ProviderAccountRef, ProviderCredentialGeneration: policy.ProviderCredentialGeneration, ConfigDigestSHA256: policy.DigestSHA256, ExpiresAt: now.Add(time.Minute)}}
	provider := &fakeProvider{text: "  распознанный текст  "}
	service, err := New(policies, credentials, provider, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	text, err := service.Transcribe(t.Context(), Input{Principal: validPrincipal(now), RequestID: "req_1", Audio: pcmWAV(time.Second), MediaType: "audio/wav"})
	if err != nil || text != "распознанный текст" {
		t.Fatalf("неожиданный результат: %q, %v", text, err)
	}
	if policies.calls != 1 || credentials.calls != 1 || provider.calls != 1 {
		t.Fatal("нарушен единичный сквозной вызов")
	}
	if provider.request.Model != value.DefaultModel || provider.request.Language != value.DefaultLanguage || provider.request.Audio.Duration != time.Second {
		t.Fatal("provider получил непроверенную конфигурацию")
	}
	for _, octet := range credentialBytes {
		if octet != 0 {
			t.Fatal("credential не очищен после provider-вызова")
		}
	}
}

func TestTranscribeFailsClosedBeforeProvider(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*Input, *value.Policy, *value.Credential)
		target error
	}{
		{name: "нет полномочия", mutate: func(input *Input, _ *value.Policy, _ *value.Credential) { input.Principal.Permission = "stt.read" }, target: errs.ErrPermissionDenied},
		{name: "неверная модель", mutate: func(_ *Input, policy *value.Policy, _ *value.Credential) { policy.Model = "gpt-4o-transcribe" }, target: errs.ErrGrantRevoked},
		{name: "подмена media type", mutate: func(input *Input, _ *value.Policy, _ *value.Credential) { input.MediaType = "audio/mpeg" }, target: errs.ErrUnsupportedAudio},
		{name: "слишком большая запись", mutate: func(input *Input, policy *value.Policy, _ *value.Credential) {
			policy.MaximumAudioBytes = 1024
			input.Audio = make([]byte, 1025)
		}, target: errs.ErrAudioTooLarge},
		{name: "слишком длинная запись", mutate: func(input *Input, policy *value.Policy, _ *value.Credential) {
			policy.MaximumAudioDuration = time.Second
			input.Audio = pcmWAV(2 * time.Second)
		}, target: errs.ErrAudioTooLong},
		{name: "устаревшее поколение credential", mutate: func(_ *Input, policy *value.Policy, credential *value.Credential) {
			credential.ProviderCredentialGeneration = policy.ProviderCredentialGeneration + 1
		}, target: errs.ErrGrantRevoked},
		{name: "credential переживает policy", mutate: func(_ *Input, policy *value.Policy, credential *value.Credential) {
			credential.ExpiresAt = policy.ExpiresAt.Add(time.Second)
		}, target: errs.ErrGrantRevoked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := validPolicy(now)
			credential := value.Credential{APIKey: []byte("test-only-credential"), ProviderAccountRef: policy.ProviderAccountRef, ProviderCredentialGeneration: policy.ProviderCredentialGeneration, ConfigDigestSHA256: policy.DigestSHA256, ExpiresAt: now.Add(time.Minute)}
			input := Input{Principal: validPrincipal(now), RequestID: "req_1", Audio: pcmWAV(time.Second), MediaType: "audio/wav"}
			test.mutate(&input, &policy, &credential)
			provider := &fakeProvider{text: "нельзя вызвать"}
			service, err := New(&fakePolicy{value: policy}, &fakeCredential{value: credential}, provider, 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			service.now = func() time.Time { return now }
			_, err = service.Transcribe(t.Context(), input)
			if !errors.Is(err, test.target) {
				t.Fatalf("ожидалась %v, получена %v", test.target, err)
			}
			if provider.calls != 0 {
				t.Fatal("provider вызван после закрытого отказа")
			}
		})
	}
}

func TestTranscribeMapsProviderFailureWithoutDiagnostics(t *testing.T) {
	now := time.Now().UTC()
	policy := validPolicy(now)
	provider := &fakeProvider{err: errors.Join(errs.ErrProviderUnavailable, errors.New("upstream detail must remain internal"))}
	service, _ := New(&fakePolicy{value: policy}, &fakeCredential{value: value.Credential{APIKey: []byte("test-only-credential"), ProviderAccountRef: policy.ProviderAccountRef, ProviderCredentialGeneration: policy.ProviderCredentialGeneration, ConfigDigestSHA256: policy.DigestSHA256, ExpiresAt: now.Add(time.Minute)}}, provider, 10*time.Second)
	service.now = func() time.Time { return now }
	_, err := service.Transcribe(t.Context(), Input{Principal: validPrincipal(now), RequestID: "req_1", Audio: pcmWAV(time.Second), MediaType: "audio/wav"})
	if !errors.Is(err, errs.ErrProviderUnavailable) {
		t.Fatalf("неверная классификация: %v", err)
	}
}

func TestTranscribeRequiresBoundedProviderDeadline(t *testing.T) {
	now := time.Now().UTC()
	policy := validPolicy(now)
	provider := &deadlineProvider{}
	service, _ := New(&fakePolicy{value: policy}, &fakeCredential{value: value.Credential{
		APIKey: []byte("test-only-credential"), ProviderAccountRef: policy.ProviderAccountRef,
		ProviderCredentialGeneration: policy.ProviderCredentialGeneration, ConfigDigestSHA256: policy.DigestSHA256,
		ExpiresAt: now.Add(time.Minute),
	}}, provider, 10*time.Second)
	service.now = func() time.Time { return now }
	_, err := service.Transcribe(t.Context(), Input{Principal: validPrincipal(now), RequestID: "req_1", Audio: pcmWAV(time.Second), MediaType: "audio/wav"})
	if !errors.Is(err, errs.ErrProviderUnavailable) || !provider.bounded {
		t.Fatalf("provider deadline не доказан: bounded=%v err=%v", provider.bounded, err)
	}
}

func TestTranscribeFailsClosedWhenProjectionIsUnavailable(t *testing.T) {
	now := time.Now().UTC()
	policy := validPolicy(now)
	tests := []struct {
		name            string
		policyError     error
		credentialError error
	}{
		{name: "системная конфигурация отсутствует", policyError: errors.New("projection unavailable")},
		{name: "credential не готов", credentialError: errors.New("projection unavailable")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeProvider{text: "нельзя вызвать"}
			service, _ := New(&fakePolicy{value: policy, err: test.policyError}, &fakeCredential{value: value.Credential{
				APIKey: []byte("test-only-credential"), ProviderAccountRef: policy.ProviderAccountRef,
				ProviderCredentialGeneration: policy.ProviderCredentialGeneration, ConfigDigestSHA256: policy.DigestSHA256,
				ExpiresAt: now.Add(time.Minute),
			}, err: test.credentialError}, provider, 10*time.Second)
			service.now = func() time.Time { return now }
			_, err := service.Transcribe(t.Context(), Input{Principal: validPrincipal(now), RequestID: "req_1", Audio: pcmWAV(time.Second), MediaType: "audio/wav"})
			if err == nil || provider.calls != 0 {
				t.Fatalf("проекция не закрыла provider path: %v", err)
			}
		})
	}
}

type deadlineProvider struct{ bounded bool }

func (provider *deadlineProvider) Transcribe(ctx context.Context, _ value.ProviderRequest) (string, error) {
	_, provider.bounded = ctx.Deadline()
	return "", context.DeadlineExceeded
}
func (*deadlineProvider) Check(context.Context) error { return nil }

func TestTranscribeCapsDeadlineByAuthorityExpiry(t *testing.T) {
	now := time.Now().UTC()
	policy := validPolicy(now)
	provider := &capturingDeadlineProvider{}
	service, _ := New(&fakePolicy{value: policy}, &fakeCredential{value: value.Credential{
		APIKey: []byte("test-only-credential"), ProviderAccountRef: policy.ProviderAccountRef,
		ProviderCredentialGeneration: policy.ProviderCredentialGeneration, ConfigDigestSHA256: policy.DigestSHA256,
		ExpiresAt: policy.ExpiresAt,
	}}, provider, 10*time.Second)
	service.now = func() time.Time { return now }
	principal := validPrincipal(now)
	principal.ExpiresAt = now.Add(500 * time.Millisecond)
	_, _ = service.Transcribe(t.Context(), Input{Principal: principal, RequestID: "req_1", Audio: pcmWAV(time.Second), MediaType: "audio/wav"})
	if provider.deadline.IsZero() || provider.deadline.After(principal.ExpiresAt) {
		t.Fatalf("deadline вышел за authority expiry: %v > %v", provider.deadline, principal.ExpiresAt)
	}
}

type capturingDeadlineProvider struct{ deadline time.Time }

func (provider *capturingDeadlineProvider) Transcribe(ctx context.Context, _ value.ProviderRequest) (string, error) {
	provider.deadline, _ = ctx.Deadline()
	return "", context.DeadlineExceeded
}
func (*capturingDeadlineProvider) Check(context.Context) error { return nil }

func TestValidateAudioRejectsTrailingWAVDataAndHeaderOnlyFLAC(t *testing.T) {
	wav := append(pcmWAV(time.Second), 0)
	if _, err := ValidateAudio(wav, "audio/wav", len(wav), time.Minute); !errors.Is(err, errs.ErrUnsupportedAudio) {
		t.Fatalf("WAV с trailing data принят: %v", err)
	}
	flac := make([]byte, 44)
	copy(flac, "fLaC")
	flac[4] = 0x80
	flac[7] = 34
	flac[18] = 0x01
	flac[19] = 0xf4
	flac[25] = 0x01
	if _, err := ValidateAudio(flac, "audio/flac", len(flac), time.Minute); !errors.Is(err, errs.ErrUnsupportedAudio) {
		t.Fatalf("FLAC без frame принят: %v", err)
	}
}

func validPolicy(now time.Time) value.Policy {
	return value.Policy{Revision: 7, DigestSHA256: strings.Repeat("a", 64), Model: value.DefaultModel, Language: value.DefaultLanguage,
		MaximumAudioBytes: 1 << 20, MaximumAudioDuration: time.Minute, ProviderTimeout: 5 * time.Second,
		ProviderAccountRef: "provider_account_1", ProviderCredentialGeneration: 3, CredentialProjectionGrant: "grant", ExpiresAt: now.Add(time.Minute)}
}

func validPrincipal(now time.Time) value.Principal {
	return value.Principal{ActorID: "actor", TenantID: "tenant", ProjectID: "prj_abcdefgh", RequestID: "request", Permission: value.PermissionTranscribe,
		AuthorityRevision: 2, AuthorityDigestSHA256: strings.Repeat("b", 64), ExpiresAt: now.Add(time.Minute)}
}

func pcmWAV(duration time.Duration) []byte {
	const sampleRate = 8000
	dataSize := int(duration * sampleRate / time.Second * 2)
	raw := make([]byte, 44+dataSize)
	copy(raw[0:4], "RIFF")
	binary.LittleEndian.PutUint32(raw[4:8], uint32(len(raw)-8))
	copy(raw[8:12], "WAVE")
	copy(raw[12:16], "fmt ")
	binary.LittleEndian.PutUint32(raw[16:20], 16)
	binary.LittleEndian.PutUint16(raw[20:22], 1)
	binary.LittleEndian.PutUint16(raw[22:24], 1)
	binary.LittleEndian.PutUint32(raw[24:28], sampleRate)
	binary.LittleEndian.PutUint32(raw[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(raw[32:34], 2)
	binary.LittleEndian.PutUint16(raw[34:36], 16)
	copy(raw[36:40], "data")
	binary.LittleEndian.PutUint32(raw[40:44], uint32(dataSize))
	return raw
}
