package projection

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	internalrpcauthorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// allowBinder counts delegation attempts and returns the original test context without generating a real continuation grant.
// RU: allowBinder считает попытки делегирования и возвращает исходный тестовый контекст без создания настоящего continuation grant.
type allowBinder struct{ calls int }

// BindDelegated records one synthetic binding and allows the fixture RPC path to proceed unchanged.
// RU: BindDelegated фиксирует одну синтетическую привязку и разрешает неизменный RPC путь оснастки.
func (binder *allowBinder) BindDelegated(ctx context.Context, _ value.Principal, _, _, _, _ string) (context.Context, error) {
	binder.calls++
	return ctx, nil
}

// denyBinder models unavailable delegated proof so projection tests can assert rejection before outbound RPC.
// RU: denyBinder моделирует недоступный delegated proof для проверки отказа до исходящего RPC.
type denyBinder struct{}

// BindDelegated always returns the pending-proof sentinel and no authorized context.
// RU: BindDelegated всегда возвращает sentinel ожидания proof без разрешённого контекста.
func (denyBinder) BindDelegated(context.Context, value.Principal, string, string, string, string) (context.Context, error) {
	return nil, errs.ErrDelegatedProofPending
}

// policyClient returns a valid synthetic owner policy with an optional targeted corruption callback and records RPC count.
// RU: policyClient возвращает допустимую синтетическую политику владельца с необязательной адресной порчей ответа и считает RPC.
type policyClient struct {
	calls  int
	mutate func(*sttv1.ResolveTranscriptionPolicyResponse)
}

// ResolveTranscriptionPolicy echoes the requested locator in a fresh policy fixture before applying the test's response mutation.
// RU: ResolveTranscriptionPolicy повторяет запрошенный locator в новой оснастке политики до применения тестовой подмены ответа.
func (client *policyClient) ResolveTranscriptionPolicy(_ context.Context, request *sttv1.ResolveTranscriptionPolicyRequest, _ ...grpc.CallOption) (*sttv1.ResolveTranscriptionPolicyResponse, error) {
	client.calls++
	response := validPolicyResponse(request.GetAuthority())
	if client.mutate != nil {
		client.mutate(response)
	}
	return response, nil
}

// credentialClient records requests and projects only a synthetic key bound to the requested configuration/account metadata.
// RU: credentialClient считает запросы и проецирует только синтетический ключ, связанный с запрошенными метаданными конфигурации/аккаунта.
type credentialClient struct{ calls int }

// ProjectTranscriptionCredential copies exact requested pins and cloned authority into a short-lived synthetic credential response without a secret service.
// RU: ProjectTranscriptionCredential копирует точные запрошенные координаты и authority-копию в краткоживущий синтетический ответ реквизитов без сервиса секретов.
func (client *credentialClient) ProjectTranscriptionCredential(_ context.Context, request *sttv1.ProjectTranscriptionCredentialRequest, _ ...grpc.CallOption) (*sttv1.ProjectTranscriptionCredentialResponse, error) {
	client.calls++
	return &sttv1.ProjectTranscriptionCredentialResponse{
		ApiKey: []byte("test-only-key"), ProviderAccountRef: request.GetProviderAccountRef(),
		ProviderCredentialGeneration: request.GetProviderCredentialGeneration(), ConfigDigestSha256: request.GetConfigDigestSha256(),
		ConfigRevision: request.GetConfigRevision(), ExpiresAt: timestamppb.New(time.Now().Add(30 * time.Second)),
		Authority: proto.Clone(request.GetAuthority()).(*sttv1.DelegatedAuthorityLocator),
	}, nil
}

// TestPolicyProjectionRequiresExactAuthorityEcho accepts the exact locator and rejects changed tenant, source, actor provenance or correlation after one binding and RPC.
// RU: TestPolicyProjectionRequiresExactAuthorityEcho принимает точный locator и отклоняет подмену tenant, source, provenance автора или correlation после одной привязки и RPC.
func TestPolicyProjectionRequiresExactAuthorityEcho(t *testing.T) {
	principal := validPrincipal()
	for _, test := range []struct {
		name      string
		mutate    func(*sttv1.ResolveTranscriptionPolicyResponse)
		wantError bool
	}{
		{name: "exact", wantError: false},
		{name: "tenant", wantError: true, mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.Authority.TenantId = "other" }},
		{name: "source revision", wantError: true, mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.Authority.SourceRevision++ }},
		{name: "actor provenance", wantError: true, mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.Authority.Actor.Reference = "other" }},
		{name: "correlation", wantError: true, mutate: func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.Authority.CorrelationId = "other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, binder := &policyClient{mutate: test.mutate}, &allowBinder{}
			client, err := NewPolicy(raw, binder)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Resolve(t.Context(), principal, principal.RequestID, "correlation")
			if (err != nil) != test.wantError {
				t.Fatalf("result = %v", err)
			}
			if raw.calls != 1 || binder.calls != 1 {
				t.Fatal("нарушен единичный projection path")
			}
		})
	}
}

// TestPolicyProjectionRejectsUnsignedLimitOverflow injects maximum uint64 size, duration and timeout values to prove rejection before signed conversion.
// RU: TestPolicyProjectionRejectsUnsignedLimitOverflow подставляет максимальные uint64 размера, длительности и timeout для доказательства отказа до знакового преобразования.
func TestPolicyProjectionRejectsUnsignedLimitOverflow(t *testing.T) {
	for _, mutate := range []func(*sttv1.ResolveTranscriptionPolicyResponse){
		func(response *sttv1.ResolveTranscriptionPolicyResponse) { response.MaximumAudioBytes = math.MaxUint64 },
		func(response *sttv1.ResolveTranscriptionPolicyResponse) {
			response.MaximumAudioDurationMilliseconds = math.MaxUint64
		},
		func(response *sttv1.ResolveTranscriptionPolicyResponse) {
			response.ProviderTimeoutMilliseconds = math.MaxUint64
		},
	} {
		raw, binder := &policyClient{mutate: mutate}, &allowBinder{}
		client, _ := NewPolicy(raw, binder)
		if _, err := client.Resolve(t.Context(), validPrincipal(), validPrincipal().RequestID, "correlation"); err == nil {
			t.Fatal("uint64 limit overflow принят")
		}
	}
}

// TestProjectionRequiresDelegatedProofBeforeRPC ensures a denied binder returns pending proof without calling the synthetic policy client.
// RU: TestProjectionRequiresDelegatedProofBeforeRPC проверяет возврат ожидания proof запрещающим binder без вызова синтетического клиента политики.
func TestProjectionRequiresDelegatedProofBeforeRPC(t *testing.T) {
	raw := &policyClient{}
	client, _ := NewPolicy(raw, denyBinder{})
	_, err := client.Resolve(t.Context(), validPrincipal(), validPrincipal().RequestID, "correlation")
	if !errors.Is(err, errs.ErrDelegatedProofPending) || raw.calls != 0 {
		t.Fatalf("outbound RPC не закрыт до proof: calls=%d err=%v", raw.calls, err)
	}
}

// TestCredentialProjectionUsesSameAuthorityLocator checks one delegated binding and RPC with preserved requested credential generation.
// RU: TestCredentialProjectionUsesSameAuthorityLocator проверяет одну delegated-привязку и RPC с сохранением запрошенного поколения реквизитов.
func TestCredentialProjectionUsesSameAuthorityLocator(t *testing.T) {
	principal := validPrincipal()
	policy := value.Policy{Revision: 7, DigestSHA256: strings.Repeat("b", 64), ProviderAccountRef: "account", ProviderCredentialGeneration: 3}
	raw, binder := &credentialClient{}, &allowBinder{}
	client, _ := NewCredential(raw, binder)
	credential, err := client.Project(t.Context(), principal, principal.RequestID, "correlation", policy)
	if err != nil || raw.calls != 1 || binder.calls != 1 || credential.ProviderCredentialGeneration != 3 {
		t.Fatalf("credential projection result: %#v, %v", credential, err)
	}
}

// validPolicyResponse creates bounded synthetic policy limits and clones the supplied authority so corruption tests do not mutate the request.
// RU: validPolicyResponse создаёт ограниченные синтетические лимиты политики и копирует authority, чтобы отрицательные тесты не изменяли запрос.
func validPolicyResponse(authority *sttv1.DelegatedAuthorityLocator) *sttv1.ResolveTranscriptionPolicyResponse {
	return &sttv1.ResolveTranscriptionPolicyResponse{
		ConfigRevision: 7, ConfigDigestSha256: strings.Repeat("b", 64), Model: value.DefaultModel, Language: value.DefaultLanguage,
		MaximumAudioBytes: 1024, MaximumAudioDurationMilliseconds: 1000, ProviderTimeoutMilliseconds: 1000,
		ProviderAccountRef: "account", ProviderCredentialGeneration: 3,
		ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)), Authority: proto.Clone(authority).(*sttv1.DelegatedAuthorityLocator),
	}
}

// validPrincipal supplies synthetic actor/tenant/project provenance, a request locator and a future expiry for projection mapping tests.
// RU: validPrincipal задаёт синтетические provenance actor/tenant/project, locator запроса и будущий срок для тестов отображения проекций.
func validPrincipal() value.Principal {
	provenance := value.AuthorityProvenance{Source: int32(internalrpcauthorityv1.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE), Reference: "source", Revision: 2, DigestSHA256: strings.Repeat("a", 64)}
	return value.Principal{
		ActorID: "actor", TenantID: "tenant", ProjectID: "prj_abcdefgh", Actor: provenance, Tenant: provenance, Project: provenance,
		RequestID: "95ec3bbc-a719-4de0-9a44-c9790c659c66", AuthorityRevision: 4,
		AuthorityDigestSHA256: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(time.Minute),
	}
}
