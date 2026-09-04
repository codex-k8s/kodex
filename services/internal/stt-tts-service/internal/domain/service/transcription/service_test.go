package transcription

import (
	"bytes"
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
	policy value.Policy
	err    error
	calls  int
}

func (fake *fakePolicy) Resolve(context.Context, value.Principal, string, string) (value.Policy, error) {
	fake.calls++
	return fake.policy, fake.err
}

type fakeCredential struct {
	credential value.Credential
	err        error
	calls      int
}

func (fake *fakeCredential) Project(context.Context, value.Principal, string, string, value.Policy) (value.Credential, error) {
	fake.calls++
	return fake.credential, fake.err
}

type fakeProvider struct {
	text        string
	err         error
	calls       int
	request     value.ProviderRequest
	localCalls  int
	egressCalls int
}

func (fake *fakeProvider) Transcribe(_ context.Context, request value.ProviderRequest) (string, error) {
	fake.calls++
	fake.request = request
	return fake.text, fake.err
}
func (fake *fakeProvider) CheckLocal(context.Context) error  { fake.localCalls++; return nil }
func (fake *fakeProvider) CheckEgress(context.Context) error { fake.egressCalls++; return nil }

type observed struct {
	stage value.Stage
	class value.ErrorClass
	calls int
}

func (observer *observed) Observe(stage value.Stage, class value.ErrorClass) {
	observer.stage, observer.class = stage, class
	observer.calls++
}

func TestTranscribeCompletePath(t *testing.T) {
	now := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	policy := validPolicy(now)
	key := []byte("test-only-credential")
	policies := &fakePolicy{policy: policy}
	credentials := &fakeCredential{credential: validCredential(policy, key, now)}
	provider, observer := &fakeProvider{text: "  распознанный текст  "}, &observed{}
	service, err := New(policies, credentials, provider, observer, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	raw := pcmWAV(time.Second)
	result, err := service.Transcribe(t.Context(), validInput(now, raw))
	if err != nil || result.Text != "распознанный текст" || result.Receipt.CompletedStage != value.StageSuccess {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if policies.calls != 1 || credentials.calls != 1 || provider.calls != 1 || observer.calls != 1 || observer.stage != value.StageSuccess {
		t.Fatal("нарушена single-effect цепочка или observability")
	}
	if result.Receipt.RequestID == "" || result.Receipt.CorrelationID == "" || result.Receipt.ProviderAccountRef == "" ||
		strings.Contains(result.Receipt.ConfigDigestSHA256, result.Text) {
		t.Fatal("receipt неполон или содержит недопустимое значение")
	}
	for _, octet := range key {
		if octet != 0 {
			t.Fatal("credential не очищен")
		}
	}
}

func TestTranscribeFailsClosedBeforeProvider(t *testing.T) {
	now := time.Now().UTC()
	for _, test := range []struct {
		name                     string
		policyErr, credentialErr error
		mutate                   func(*Input, *value.Policy)
	}{
		{name: "нет полномочия", mutate: func(input *Input, _ *value.Policy) { input.Principal.Permission = "stt.read" }},
		{name: "continuation proof отсутствует", policyErr: errs.ErrDelegatedProofPending},
		{name: "policy отсутствует", policyErr: errors.New("unavailable")},
		{name: "credential отсутствует", credentialErr: errors.New("unavailable")},
		{name: "неверная модель", mutate: func(_ *Input, policy *value.Policy) { policy.Model = "other" }},
		{name: "слишком большой файл", mutate: func(input *Input, policy *value.Policy) { policy.MaximumAudioBytes = 1024; input.AudioSizeBytes = 1025 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := validPolicy(now)
			raw := pcmWAV(time.Second)
			input := validInput(now, raw)
			if test.mutate != nil {
				test.mutate(&input, &policy)
			}
			provider, observer := &fakeProvider{text: "не вызывать"}, &observed{}
			service, _ := New(&fakePolicy{policy: policy, err: test.policyErr}, &fakeCredential{credential: validCredential(policy, []byte("test-only-key"), now), err: test.credentialErr}, provider, observer, 10*time.Second)
			service.now = func() time.Time { return now }
			if _, err := service.Transcribe(t.Context(), input); err == nil || provider.calls != 0 || observer.calls != 1 {
				t.Fatalf("fail-closed нарушен: calls=%d observed=%d err=%v", provider.calls, observer.calls, err)
			}
		})
	}
}

func TestTranscribeCapsDeadlineByAuthorityExpiry(t *testing.T) {
	now := time.Now().UTC()
	policy := validPolicy(now)
	provider := &deadlineProvider{}
	service, _ := New(&fakePolicy{policy: policy}, &fakeCredential{credential: validCredential(policy, []byte("test-only-key"), now)}, provider, &observed{}, 10*time.Second)
	service.now = func() time.Time { return now }
	raw := pcmWAV(time.Second)
	input := validInput(now, raw)
	input.Principal.ExpiresAt = now.Add(500 * time.Millisecond)
	_, _ = service.Transcribe(t.Context(), input)
	if provider.deadline.IsZero() || provider.deadline.After(input.Principal.ExpiresAt) {
		t.Fatal("deadline вышел за authority expiry")
	}
}

func TestTranscribeDoesNotWidenTransportDeadline(t *testing.T) {
	now := time.Now().UTC()
	policy := validPolicy(now)
	provider := &deadlineProvider{}
	service, _ := New(&fakePolicy{policy: policy}, &fakeCredential{credential: validCredential(policy, []byte("test-only-key"), now)}, provider, &observed{}, 10*time.Second)
	service.now = func() time.Time { return now }
	raw := pcmWAV(time.Second)
	transportDeadline := time.Now().Add(200 * time.Millisecond)
	ctx, cancel := context.WithDeadline(t.Context(), transportDeadline)
	defer cancel()
	_, _ = service.Transcribe(ctx, validInput(now, raw))
	if provider.deadline.IsZero() || provider.deadline.After(transportDeadline) {
		t.Fatalf("provider deadline=%v шире transport deadline=%v", provider.deadline, transportDeadline)
	}
}

func TestReadinessAndDiagnosticHaveNoRemoteEffect(t *testing.T) {
	now := time.Now().UTC()
	policy := validPolicy(now)
	provider := &fakeProvider{text: "не вызывать"}
	service, _ := New(&fakePolicy{policy: policy}, &fakeCredential{credential: validCredential(policy, []byte("test-only-key"), now)}, provider, &observed{}, 10*time.Second)
	if err := service.CheckLocal(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(service.CheckProtectedPath(t.Context()), errs.ErrDelegatedProofPending) {
		t.Fatal("diagnostic не остановился на delegated authority")
	}
	if provider.localCalls != 1 || provider.egressCalls != 0 || provider.calls != 0 {
		t.Fatalf("readiness/diagnostic вызвали remote effect: local=%d egress=%d provider=%d", provider.localCalls, provider.egressCalls, provider.calls)
	}
}

type deadlineProvider struct{ deadline time.Time }

func (provider *deadlineProvider) Transcribe(ctx context.Context, _ value.ProviderRequest) (string, error) {
	provider.deadline, _ = ctx.Deadline()
	return "", context.DeadlineExceeded
}
func (*deadlineProvider) CheckLocal(context.Context) error  { return nil }
func (*deadlineProvider) CheckEgress(context.Context) error { return nil }

func TestValidateAudioRejectsTrailingWAVDataAndHeaderOnlyFLAC(t *testing.T) {
	wav := append(pcmWAV(time.Second), 0)
	if _, err := ValidateAudio(bytes.NewReader(wav), int64(len(wav)), "audio/wav", int64(len(wav)), time.Minute); !errors.Is(err, errs.ErrUnsupportedAudio) {
		t.Fatalf("WAV trailing data принят: %v", err)
	}
	for _, flac := range [][]byte{falseFLACTotalSamples(), overflowFLACTotalSamples(), append(falseFLACTotalSamples(), []byte("trailing")...)} {
		if _, err := ValidateAudio(bytes.NewReader(flac), int64(len(flac)), "audio/flac", int64(len(flac)), time.Minute); !errors.Is(err, errs.ErrUnsupportedAudio) {
			t.Fatalf("непроверенный FLAC принят: %v", err)
		}
	}
	mp3 := make([]byte, 104)
	copy(mp3, []byte{0xff, 0xfb, 0x10, 0x00})
	if _, err := ValidateAudio(bytes.NewReader(mp3), int64(len(mp3)), "audio/mpeg", int64(len(mp3)), time.Minute); err != nil {
		t.Fatalf("валидный MP3 отклонён: %v", err)
	}
	mp3 = append(mp3, 0)
	if _, err := ValidateAudio(bytes.NewReader(mp3), int64(len(mp3)), "audio/mpeg", int64(len(mp3)), time.Minute); !errors.Is(err, errs.ErrUnsupportedAudio) {
		t.Fatalf("MP3 trailing data принят: %v", err)
	}
}

func validPolicy(now time.Time) value.Policy {
	return value.Policy{Revision: 7, DigestSHA256: strings.Repeat("a", 64), Model: value.DefaultModel, Language: value.DefaultLanguage,
		MaximumAudioBytes: 1 << 20, MaximumAudioDuration: time.Minute, ProviderTimeout: 5 * time.Second,
		ProviderAccountRef: "provider_account_1", ProviderCredentialGeneration: 3, ExpiresAt: now.Add(time.Minute)}
}

func validCredential(policy value.Policy, key []byte, now time.Time) value.Credential {
	return value.Credential{APIKey: key, ProviderAccountRef: policy.ProviderAccountRef, ProviderCredentialGeneration: policy.ProviderCredentialGeneration,
		ConfigDigestSHA256: policy.DigestSHA256, ExpiresAt: now.Add(30 * time.Second)}
}

func validInput(now time.Time, raw []byte) Input {
	principal := value.Principal{ActorID: "actor", TenantID: "tenant", ProjectID: "project", RequestID: "request", Permission: value.PermissionTranscribe,
		AuthorityRevision: 2, AuthorityDigestSHA256: strings.Repeat("b", 64), ExpiresAt: now.Add(time.Minute)}
	return Input{Principal: principal, RequestID: principal.RequestID, CorrelationID: "correlation", AudioReader: bytes.NewReader(raw), AudioSizeBytes: int64(len(raw)), MediaType: "audio/wav"}
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

func falseFLACTotalSamples() []byte {
	raw := make([]byte, 44)
	copy(raw, "fLaC")
	raw[4] = 0x80
	raw[7] = 34
	raw[18] = 0x0a
	raw[19] = 0xc4
	raw[20] = 0x40
	raw[21] = 0x00
	raw[25] = 1
	raw[42], raw[43] = 0xff, 0xf8
	return raw
}
func overflowFLACTotalSamples() []byte {
	raw := falseFLACTotalSamples()
	for index := 21; index <= 25; index++ {
		raw[index] = 0xff
	}
	return raw
}
