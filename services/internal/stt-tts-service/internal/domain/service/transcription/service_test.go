package transcription

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/audio/ffmpeg"
)

// fakePolicy counts policy projection calls and can return an exact synthetic failure.
// fakePolicy считает обращения за политикой и может вернуть заданный тестовый отказ.
type fakePolicy struct {
	policy value.Policy
	err    error
	calls  int
}

// Resolve returns the configured policy while recording whether the authority path reached it.
// Resolve возвращает заданную политику и отмечает достижение этого шага после проверки полномочий.
func (fake *fakePolicy) Resolve(context.Context, value.Principal, string, string) (value.Policy, error) {
	fake.calls++
	return fake.policy, fake.err
}

// fakeCredential counts managed-key projections to detect premature secret access.
// fakeCredential считает обращения за управляемым ключом для выявления преждевременного доступа.
type fakeCredential struct {
	credential value.Credential
	err        error
	calls      int
}

// Project returns a synthetic key only when the service reaches credential projection.
// Project возвращает тестовый ключ лишь при достижении сервисом шага получения реквизита.
func (fake *fakeCredential) Project(context.Context, value.Principal, string, string, value.Policy) (value.Credential, error) {
	fake.calls++
	return fake.credential, fake.err
}

// fakeProvider records recognition and readiness calls separately to detect
// an external effect before policy, audio and credential validation.
// RU: fakeProvider раздельно считает вызовы распознавания и готовности, чтобы
// обнаружить внешний эффект до проверки политики, аудио и реквизита.
type fakeProvider struct {
	text        string
	err         error
	calls       int
	request     value.ProviderRequest
	localCalls  int
	egressCalls int
}

// Transcribe captures the exact provider request and returns configured text or failure.
// Transcribe сохраняет точный запрос к провайдеру и возвращает заданный текст или отказ.
func (fake *fakeProvider) Transcribe(_ context.Context, request value.ProviderRequest) (string, error) {
	fake.calls++
	fake.request = request
	return fake.text, fake.err
}

// CheckLocal counts local readiness without issuing a model request.
// CheckLocal считает локальную готовность без запроса к модели.
func (fake *fakeProvider) CheckLocal(context.Context) error { fake.localCalls++; return nil }

// CheckEgress counts route checks without issuing a model request.
// CheckEgress считает проверки маршрута без запроса к модели.
func (fake *fakeProvider) CheckEgress(context.Context) error { fake.egressCalls++; return nil }

// observed captures the last bounded stage/class pair and the number of emitted outcomes.
// observed сохраняет последнюю ограниченную пару этапа и класса и число зарегистрированных исходов.
type observed struct {
	stage value.Stage
	class value.ErrorClass
	calls int
}

// TestInvalidModelParametersStopBeforeCredentialAndProvider ensures invalid
// model controls cannot trigger secret projection or a billable provider call.
// RU: TestInvalidModelParametersStopBeforeCredentialAndProvider проверяет, что
// неверные параметры модели не приводят к получению секрета и платному вызову.
func TestInvalidModelParametersStopBeforeCredentialAndProvider(t *testing.T) {
	now := time.Now()
	for _, parameters := range []modelprofile.Parameters{{Stream: true}, {Keywords: []string{"bad\nterm"}}, {Temperature: 2}, {Languages: []string{"ru", "en"}}} {
		policy := validPolicy(now)
		policy.Parameters = parameters
		policies, credentials, provider := &fakePolicy{policy: policy}, &fakeCredential{}, &fakeProvider{}
		service, err := New(policies, credentials, provider, &observed{}, 10*time.Second, ffmpeg.New(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Transcribe(t.Context(), validInput(now, pcmWAV(time.Second))); err == nil || credentials.calls != 0 || provider.calls != 0 {
			t.Fatal("invalid parameters reached a downstream effect")
		}
	}
}

// Observe records each closed outcome without accepting a request payload.
// Observe фиксирует каждый закрытый исход без получения тела запроса.
func (observer *observed) Observe(stage value.Stage, class value.ErrorClass) {
	observer.stage, observer.class = stage, class
	observer.calls++
}

// TestTranscribeCompletePath checks one policy, credential, provider and
// observation effect, trimmed text, receipt pins and key zeroing on success.
// RU: TestTranscribeCompletePath проверяет по одному действию на каждом шаге,
// очищенный текст, закреплённую квитанцию и обнуление ключа после успеха.
func TestTranscribeCompletePath(t *testing.T) {
	now := time.Now().UTC()
	policy := validPolicy(now)
	key := []byte("test-only-credential")
	policies := &fakePolicy{policy: policy}
	credentials := &fakeCredential{credential: validCredential(policy, key, now)}
	provider, observer := &fakeProvider{text: "  распознанный текст  "}, &observed{}
	service, err := New(policies, credentials, provider, observer, 10*time.Second, ffmpeg.New(t.TempDir()))
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

// TestTranscribeFailsClosedBeforeProvider rejects absent authority, policy,
// credential and valid media limits before any model invocation.
// RU: TestTranscribeFailsClosedBeforeProvider отклоняет отсутствие полномочия,
// политики, реквизита и допустимых лимитов аудио до вызова модели.
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
			service, _ := New(&fakePolicy{policy: policy, err: test.policyErr}, &fakeCredential{credential: validCredential(policy, []byte("test-only-key"), now), err: test.credentialErr}, provider, observer, 10*time.Second, ffmpeg.New(t.TempDir()))
			service.now = func() time.Time { return now }
			if _, err := service.Transcribe(t.Context(), input); err == nil || provider.calls != 0 || observer.calls != 1 {
				t.Fatalf("fail-closed нарушен: calls=%d observed=%d err=%v", provider.calls, observer.calls, err)
			}
		})
	}
}

// TestTranscribeCapsDeadlineByAuthorityExpiry ensures the provider context
// cannot outlive the verified principal's authority.
// RU: TestTranscribeCapsDeadlineByAuthorityExpiry проверяет, что контекст модели
// не переживает срок полномочия проверенного субъекта.
func TestTranscribeCapsDeadlineByAuthorityExpiry(t *testing.T) {
	now := time.Now().UTC()
	service, provider, input := deadlineFixture(t, now)
	input.Principal.ExpiresAt = now.Add(500 * time.Millisecond)
	_, _ = service.Transcribe(t.Context(), input)
	if provider.deadline.IsZero() || provider.deadline.After(input.Principal.ExpiresAt) {
		t.Fatal("deadline вышел за authority expiry")
	}
}

// TestTranscribeDoesNotWidenTransportDeadline preserves the incoming request's
// shorter deadline across policy, credential and provider calls.
// RU: TestTranscribeDoesNotWidenTransportDeadline сохраняет более короткий срок
// входящего запроса на путях политики, реквизита и провайдера.
func TestTranscribeDoesNotWidenTransportDeadline(t *testing.T) {
	now := time.Now().UTC()
	service, provider, input := deadlineFixture(t, now)
	transportDeadline := time.Now().Add(200 * time.Millisecond)
	ctx, cancel := context.WithDeadline(t.Context(), transportDeadline)
	defer cancel()
	_, _ = service.Transcribe(ctx, input)
	if provider.deadline.IsZero() || provider.deadline.After(transportDeadline) {
		t.Fatalf("provider deadline=%v шире transport deadline=%v", provider.deadline, transportDeadline)
	}
}

// TestReadinessAndDiagnosticHaveNoRemoteEffect distinguishes local readiness
// from delegated proof and forbids egress or recognition during both checks.
// RU: TestReadinessAndDiagnosticHaveNoRemoteEffect отличает локальную готовность
// от делегированного доказательства и запрещает сетевой или модельный вызов.
func TestReadinessAndDiagnosticHaveNoRemoteEffect(t *testing.T) {
	now := time.Now().UTC()
	policy := validPolicy(now)
	provider := &fakeProvider{text: "не вызывать"}
	service, _ := New(&fakePolicy{policy: policy}, &fakeCredential{credential: validCredential(policy, []byte("test-only-key"), now)}, provider, &observed{}, 10*time.Second, ffmpeg.New(t.TempDir()))
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

// deadlineProvider captures the actual context deadline observed by the model adapter.
// deadlineProvider сохраняет фактический срок контекста, увиденный адаптером модели.
type deadlineProvider struct{ deadline time.Time }

// deadlineFixture builds the same pinned policy, credential and seekable audio
// for both deadline tests while preserving each test's own caller deadline.
// RU: deadlineFixture собирает одинаковые закреплённые политику, реквизит и аудио
// для двух тестов срока, оставляя каждому собственный срок вызывающего.
func deadlineFixture(t *testing.T, now time.Time) (*Service, *deadlineProvider, Input) {
	t.Helper()
	policy := validPolicy(now)
	provider := &deadlineProvider{}
	service, err := New(&fakePolicy{policy: policy}, &fakeCredential{credential: validCredential(policy, []byte("test-only-key"), now)}, provider, &observed{}, 10*time.Second, ffmpeg.New(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	return service, provider, validInput(now, pcmWAV(time.Second))
}

// Transcribe stores the supplied deadline and returns a synthetic timeout.
// Transcribe сохраняет переданный срок и возвращает тестовое истечение времени.
func (provider *deadlineProvider) Transcribe(ctx context.Context, _ value.ProviderRequest) (string, error) {
	provider.deadline, _ = ctx.Deadline()
	return "", context.DeadlineExceeded
}

// CheckLocal keeps local readiness independent of the deadline assertion.
// CheckLocal оставляет локальную готовность независимой от проверки срока.
func (*deadlineProvider) CheckLocal(context.Context) error { return nil }

// CheckEgress keeps the route available so the fixture reaches the provider deadline.
// CheckEgress оставляет маршрут доступным для достижения срока провайдера в тесте.
func (*deadlineProvider) CheckEgress(context.Context) error { return nil }

// TestValidateAudioRejectsTrailingWAVDataAndHeaderOnlyFLAC rejects incomplete
// or trailing-data containers that cannot prove the duration of all bytes.
// RU: TestValidateAudioRejectsTrailingWAVDataAndHeaderOnlyFLAC отклоняет
// неполные контейнеры и лишние байты без доказанной длительности всего файла.
func TestValidateAudioRejectsTrailingWAVDataAndHeaderOnlyFLAC(t *testing.T) {
	wav := append(pcmWAV(time.Second), 0)
	if _, err := ValidateAudio(t.Context(), bytes.NewReader(wav), int64(len(wav)), "audio/wav", int64(len(wav)), time.Minute, ffmpeg.New(t.TempDir())); !errors.Is(err, errs.ErrUnsupportedAudio) {
		t.Fatalf("WAV trailing data принят: %v", err)
	}
	for _, flac := range [][]byte{falseFLACTotalSamples(), overflowFLACTotalSamples(), append(falseFLACTotalSamples(), []byte("trailing")...)} {
		if _, err := ValidateAudio(t.Context(), bytes.NewReader(flac), int64(len(flac)), "audio/flac", int64(len(flac)), time.Minute, ffmpeg.New(t.TempDir())); !errors.Is(err, errs.ErrUnsupportedAudio) {
			t.Fatalf("непроверенный FLAC принят: %v", err)
		}
	}
	mp3 := make([]byte, 104)
	copy(mp3, []byte{0xff, 0xfb, 0x10, 0x00})
	if _, err := ValidateAudio(t.Context(), bytes.NewReader(mp3), int64(len(mp3)), "audio/mpeg", int64(len(mp3)), time.Minute, ffmpeg.New(t.TempDir())); !errors.Is(err, errs.ErrUnsupportedAudio) {
		t.Fatalf("синтетический header без корректных frames принят: %v", err)
	}
	mp3 = append(mp3, 0)
	if _, err := ValidateAudio(t.Context(), bytes.NewReader(mp3), int64(len(mp3)), "audio/mpeg", int64(len(mp3)), time.Minute, ffmpeg.New(t.TempDir())); !errors.Is(err, errs.ErrUnsupportedAudio) {
		t.Fatalf("MP3 trailing data принят: %v", err)
	}
}

// validPolicy supplies a pinned, unexpired policy for focused STT path tests.
// validPolicy создаёт закреплённую действующую политику для адресных тестов STT.
func validPolicy(now time.Time) value.Policy {
	return value.Policy{Revision: 7, DigestSHA256: strings.Repeat("a", 64), Model: value.DefaultModel, Language: value.DefaultLanguage,
		MaximumAudioBytes: 1 << 20, MaximumAudioDuration: time.Minute, ProviderTimeout: 5 * time.Second,
		ProviderAccountRef: "provider_account_1", ProviderCredentialGeneration: 3, ExpiresAt: now.Add(time.Minute)}
}

// validCredential binds test key bytes to the supplied policy account and digest.
// validCredential связывает байты тестового ключа с аккаунтом и дайджестом политики.
func validCredential(policy value.Policy, key []byte, now time.Time) value.Credential {
	return value.Credential{APIKey: key, ProviderAccountRef: policy.ProviderAccountRef, ProviderCredentialGeneration: policy.ProviderCredentialGeneration,
		ConfigDigestSHA256: policy.DigestSHA256, ExpiresAt: now.Add(30 * time.Second)}
}

// validInput creates a matching request/principal pair around seekable audio bytes.
// validInput создаёт совпадающие запрос и субъект с аудио, доступным для перемотки.
func validInput(now time.Time, raw []byte) Input {
	principal := value.Principal{ActorID: "actor", TenantID: "tenant", ProjectID: "project", RequestID: "request", Permission: value.PermissionTranscribe,
		AuthorityRevision: 2, AuthorityDigestSHA256: strings.Repeat("b", 64), ExpiresAt: now.Add(time.Minute)}
	return Input{Principal: principal, RequestID: principal.RequestID, CorrelationID: "correlation", AudioReader: bytes.NewReader(raw), AudioSizeBytes: int64(len(raw)), MediaType: "audio/wav"}
}

// pcmWAV constructs a minimal mono PCM WAV whose header and sample count agree.
// pcmWAV собирает минимальный моно WAV PCM с согласованными заголовком и числом сэмплов.
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

// falseFLACTotalSamples creates a FLAC-like header with misleading sample metadata.
// falseFLACTotalSamples создаёт подобие заголовка FLAC с недостоверным числом сэмплов.
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

// overflowFLACTotalSamples mutates the fixture to claim an implausibly large sample count.
// overflowFLACTotalSamples меняет тестовый файл на неправдоподобно большое число сэмплов.
func overflowFLACTotalSamples() []byte {
	raw := falseFLACTotalSamples()
	for index := 21; index <= 25; index++ {
		raw[index] = 0xff
	}
	return raw
}
