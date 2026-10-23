package providercredentialclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc"
)

// authorizationMetadataStub records metadata-only observation calls while inheriting the independent cleanup stub.
// Its configured responses and errors remain local and do not access provider or Kubernetes state.
// authorizationMetadataStub записывает вызовы наблюдения только метаданных, наследуя отдельную заглушку очистки.
// Настроенные ответы и ошибки остаются локальными и не обращаются к состоянию провайдера или Kubernetes.
type authorizationMetadataStub struct {
	providerCredentialClientStub
	metadata        *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse
	metadataRequest *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationRequest
	metadataCalls   int
}

// ObserveDeviceAuthorization increments the metadata counter, records the exact request and returns the configured response/error.
// It does not execute device authorization or poll a real provider.
// ObserveDeviceAuthorization увеличивает счётчик метаданных, сохраняет точный запрос и возвращает настроенный ответ/ошибку.
// Авторизация устройства и опрос настоящего провайдера не выполняются.
func (stub *authorizationMetadataStub) ObserveDeviceAuthorization(_ context.Context, request *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationRequest, _ ...grpc.CallOption) (*controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse, error) {
	stub.metadataCalls++
	stub.metadataRequest = request
	return stub.metadata, stub.err
}

// authorizationCleanupTarget builds synthetic owner references and derives the materializer locator from the attempt/account pair.
// The fixture leaves kind/UID/version unset for a metadata observation to resolve.
// authorizationCleanupTarget создаёт синтетические ссылки владельца и выводит указатель размещения из пары попытки и аккаунта.
// Вид, UID и версия оставляются незаданными для разрешения наблюдением метаданных.
func authorizationCleanupTarget() entity.ProviderAuthorizationCleanupTarget {
	target := entity.ProviderAuthorizationCleanupTarget{TaskRef: cleanupTaskRef, AccountRef: cleanupAccountRef, AuthorizationAttemptRef: "pauth_" + strings.Repeat("a", 32), Generation: 7}
	digest := sha256.Sum256([]byte(target.AuthorizationAttemptRef + "\x00" + target.AccountRef))
	target.MaterializerAttemptRef = "pmat_" + hex.EncodeToString(digest[:16])
	return target
}

// TestAuthorizationMetadataPreservesExactOwnerBinding checks both absence states retain task/account/attempt/generation and metadata-only mode.
// Unfenced absence must not become confirmed, and a foreign account response must fail.
// TestAuthorizationMetadataPreservesExactOwnerBinding проверяет сохранение задачи/аккаунта/попытки/поколения и режима только метаданных для обоих видов отсутствия.
// Неограждённое отсутствие не должно становиться подтверждённым, а ответ чужого аккаунта должен отклоняться.
func TestAuthorizationMetadataPreservesExactOwnerBinding(t *testing.T) {
	t.Parallel()
	target := authorizationCleanupTarget()
	for _, state := range []controlplanev1.ProviderAuthorizationObjectState{
		controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_ABSENT_UNFENCED,
		controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_CONFIRMED_ABSENT,
	} {
		stub := &authorizationMetadataStub{metadata: &controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse{
			ObjectState: state, AbsentObject: &controlplanev1.ProviderAuthorizationAbsenceDescriptor{
				MaterializerAttemptRef: target.MaterializerAttemptRef, AccountRef: target.AccountRef, AuthorizationAttemptRef: target.AuthorizationAttemptRef,
			},
		}}
		client, err := New(&controlplaneclient.Client{ProviderCredentials: stub})
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.ObserveAuthorizationCleanup(context.Background(), target)
		if err != nil || result.Target.Kind != "AUTHORIZATION_ABSENCE" || result.Target.UID != "" || result.Target.Generation != 7 {
			t.Fatalf("metadata read failed: result=%+v err=%v", result, err)
		}
		request := stub.metadataRequest
		if request.GetMode() != controlplanev1.ProviderAuthorizationObservationMode_PROVIDER_AUTHORIZATION_OBSERVATION_MODE_METADATA_ONLY || request.GetTaskRef() != target.TaskRef || request.GetAccountRef() != target.AccountRef || request.GetAuthorizationAttemptRef() != target.AuthorizationAttemptRef || request.GetLeaseGeneration() != target.Generation {
			t.Fatal("metadata request lost owner binding")
		}
		if (state == controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_CONFIRMED_ABSENT) != (result.State == "CONFIRMED_ABSENT") {
			t.Fatal("unfenced absence was promoted to terminal")
		}
		stub.metadata.AbsentObject.AccountRef = "pacc_other_account"
		if _, err := client.ObserveAuthorizationCleanup(context.Background(), target); err == nil {
			t.Fatal("foreign account metadata accepted")
		}
	}
}

// TestAuthorizationCleanupRejectsMixedPinsBeforeRPC mutates target kind, presence/absence pins, account binding and generation.
// Each invalid target must fail before the cleanup stub records any RPC.
// TestAuthorizationCleanupRejectsMixedPinsBeforeRPC изменяет вид цели, привязки наличия/отсутствия, аккаунт и поколение.
// Каждая некорректная цель должна отклоняться до записи RPC заглушкой очистки.
func TestAuthorizationCleanupRejectsMixedPinsBeforeRPC(t *testing.T) {
	t.Parallel()
	base := authorizationCleanupTarget()
	for _, change := range []func(*entity.ProviderAuthorizationCleanupTarget){
		func(v *entity.ProviderAuthorizationCleanupTarget) { v.Kind = "UNKNOWN" },
		func(v *entity.ProviderAuthorizationCleanupTarget) {
			v.Kind = "AUTHORIZATION_ABSENCE"
			v.UID = cleanupCredential.SecretUID
		},
		func(v *entity.ProviderAuthorizationCleanupTarget) { v.Kind = "AUTHORIZATION_ATTEMPT" },
		func(v *entity.ProviderAuthorizationCleanupTarget) {
			v.Kind = "AUTHORIZATION_ABSENCE"
			v.AccountRef = "pacc_foreign_account"
		},
		func(v *entity.ProviderAuthorizationCleanupTarget) { v.Kind = "AUTHORIZATION_ABSENCE"; v.Generation = 0 },
	} {
		target := base
		change(&target)
		stub := &providerCredentialClientStub{}
		if _, err := cleanupClient(t, stub).CleanupAuthorization(context.Background(), target); err == nil || stub.calls != 0 {
			t.Fatal("invalid cleanup reached broker")
		}
	}
}

// TestAuthorizationMetadataPresentAndClosedResponse accepts an exact synthetic present object, then rejects unknown state, invalid UID and mixed or extraneous response fields.
// It checks response mapping locally rather than proving broker authorization or resource existence.
// TestAuthorizationMetadataPresentAndClosedResponse принимает точный синтетический присутствующий объект, затем отклоняет неизвестное состояние, неверный UID и смешанные или лишние поля ответа.
// Проверяется локальное преобразование ответа, а не авторизация брокера или существование ресурса.
func TestAuthorizationMetadataPresentAndClosedResponse(t *testing.T) {
	t.Parallel()
	target := authorizationCleanupTarget()
	fixture := func() *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse {
		return &controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse{
			ObjectState: controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_PRESENT,
			PendingObject: &controlplanev1.ProviderAuthorizationObjectDescriptor{
				MaterializerAttemptRef: target.MaterializerAttemptRef, AccountRef: target.AccountRef,
				AuthorizationAttemptRef: target.AuthorizationAttemptRef, Uid: cleanupCredential.SecretUID, ResourceVersion: "rv-17",
			},
		}
	}
	stub := &authorizationMetadataStub{metadata: fixture()}
	client, err := New(&controlplaneclient.Client{ProviderCredentials: stub})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.ObserveAuthorizationCleanup(context.Background(), target)
	if err != nil || result.State != "PRESENT" || result.Target.UID != cleanupCredential.SecretUID || result.Target.ResourceVersion != "rv-17" {
		t.Fatalf("present metadata lost: %v", err)
	}
	for _, mutate := range []func(*controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse){
		func(v *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse) {
			v.ObjectState = 99
		},
		func(v *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse) {
			v.PendingObject.Uid = "invalid"
		},
		func(v *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse) {
			v.AbsentObject = &controlplanev1.ProviderAuthorizationAbsenceDescriptor{}
		},
		func(v *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse) {
			v.ExternalAccountMasked = "unexpected"
		},
		func(v *controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationResponse) {
			v.State = controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_PENDING
		},
	} {
		stub.metadata = fixture()
		mutate(stub.metadata)
		if _, err := client.ObserveAuthorizationCleanup(context.Background(), target); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}

// TestAuthorizationCleanupPreservesProducedCredentialForSeparateTask verifies absence cleanup carries no pending/credential deletion target
// and returns a valid produced descriptor unchanged, while rejecting a later invalid digest.
// TestAuthorizationCleanupPreservesProducedCredentialForSeparateTask проверяет отсутствие цели удаления ожидающего объекта/учётных данных в очистке отсутствия
// и неизменный возврат корректного созданного дескриптора, отклоняя последующий неверный дайджест.
func TestAuthorizationCleanupPreservesProducedCredentialForSeparateTask(t *testing.T) {
	t.Parallel()
	target := authorizationCleanupTarget()
	target.Kind = "AUTHORIZATION_ABSENCE"
	stub := &providerCredentialClientStub{response: &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse{
		TerminalReceipt: cleanupReceipt, ProducedCredential: &controlplanev1.ProviderCredentialDescriptor{
			SecretName: cleanupCredential.SecretName, SecretUid: cleanupCredential.SecretUID, SecretResourceVersion: cleanupCredential.SecretResourceVersion, ContentSha256: cleanupCredential.ContentSHA256,
		},
	}}
	result, err := cleanupClient(t, stub).CleanupAuthorization(context.Background(), target)
	if err != nil || result.ProducedCredential == nil || *result.ProducedCredential != cleanupCredential {
		t.Fatalf("produced credential lost: %v", err)
	}
	if stub.request.GetCredential() != nil || stub.request.GetPendingObject() != nil || stub.request.GetAbsentObject() == nil || stub.request.GetTargetKind() != controlplanev1.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_AUTHORIZATION_ABSENCE {
		t.Fatal("absence cleanup was encoded as delete")
	}
	stub.response.ProducedCredential.ContentSha256 = "invalid"
	if _, err := cleanupClient(t, stub).CleanupAuthorization(context.Background(), target); err == nil {
		t.Fatal("invalid produced credential accepted")
	}
}
