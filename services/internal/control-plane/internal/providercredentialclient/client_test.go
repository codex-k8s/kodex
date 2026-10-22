package providercredentialclient

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

const (
	// cleanupTaskRef identifies a synthetic cleanup task used to assert exact RPC request mapping.
	// cleanupTaskRef обозначает синтетическую задачу очистки для проверки точного преобразования запроса RPC.
	cleanupTaskRef = "pcct_61000000-0000-4000-8000-000000000001"
	// cleanupAccountRef supplies a bounded synthetic account reference, not access to an actual provider account.
	// cleanupAccountRef задаёт ограниченную синтетическую ссылку аккаунта, а не доступ к настоящему аккаунту провайдера.
	cleanupAccountRef = "pacc_cleanup_Account9Z"
	// cleanupReceipt is an opaque synthetic successful response used to test receipt propagation, not signature verification.
	// cleanupReceipt является непрозрачным синтетическим успешным ответом для проверки передачи квитанции, а не её подписи.
	cleanupReceipt = "provider-credential-cleanup:61000000-0000-4000-8000-000000000001:g7"
)

// cleanupCredential supplies valid synthetic Secret metadata shared by cleanup tests.
// Mutation cases copy this value before changing individual pins.
// cleanupCredential задаёт корректные синтетические метаданные Secret для тестов очистки.
// Для проверок изменений значение копируется перед заменой отдельных привязок.
var cleanupCredential = entity.ProviderCredentialDescriptor{
	SecretName: "provider-credential-1", SecretUID: "61000000-0000-4000-8000-000000000002",
	SecretResourceVersion: "cleanup-7", ContentSHA256: strings.Repeat("a", 64),
}

// providerCredentialClientStub records cleanup calls and returns a configured response or error without network access.
// Other generated RPC methods remain embedded and are not exercised by these tests.
// providerCredentialClientStub записывает вызовы очистки и возвращает настроенный ответ или ошибку без доступа к сети.
// Остальные сгенерированные методы RPC встроены и этими тестами не вызываются.
type providerCredentialClientStub struct {
	controlplanev1.ProviderCredentialMaterializerServiceClient
	response *controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse
	err      error
	request  *controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest
	calls    int
}

// CleanupProviderCredential captures the request, increments the call counter and returns the configured stub result.
// It performs no cleanup or server-side authorization.
// CleanupProviderCredential сохраняет запрос, увеличивает счётчик вызовов и возвращает настроенный результат подставного клиента.
// Очистка и серверная авторизация не выполняются.
func (client *providerCredentialClientStub) CleanupProviderCredential(
	_ context.Context,
	request *controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest,
	_ ...grpc.CallOption,
) (*controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse, error) {
	client.calls++
	client.request = request
	return client.response, client.err
}

// cleanupClient constructs the domain adapter around a stubbed provider-credential client and fails the test on constructor error.
// No transport connection or real materializer is created.
// cleanupClient создаёт доменный адаптер вокруг подставного клиента учётных данных и прерывает тест при ошибке конструктора.
// Транспортное соединение и настоящий сервис размещения не создаются.
func cleanupClient(t *testing.T, stub *providerCredentialClientStub) *Client {
	t.Helper()
	client, err := New(&controlplaneclient.Client{ProviderCredentials: stub})
	if err != nil {
		t.Fatalf("construct provider credential client: %v", err)
	}
	return client
}

// TestCleanupProviderCredentialSendsExactDescriptorAndReturnsReceipt verifies one RPC carries the current task,
// account, generation and credential pins, and maps the returned terminal receipt.
// TestCleanupProviderCredentialSendsExactDescriptorAndReturnsReceipt проверяет передачу текущей задачи,
// аккаунта, поколения и привязок учётных данных одним RPC и преобразование итоговой квитанции.
func TestCleanupProviderCredentialSendsExactDescriptorAndReturnsReceipt(t *testing.T) {
	t.Parallel()
	stub := &providerCredentialClientStub{
		response: &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse{
			TerminalReceipt: cleanupReceipt,
		},
	}
	receipt, err := cleanupClient(t, stub).CleanupProviderCredential(
		context.Background(), cleanupTaskRef, cleanupAccountRef, 7, cleanupCredential,
	)
	if err != nil || receipt.TerminalReceipt != cleanupReceipt {
		t.Fatalf("cleanup result: receipt=%+v err=%v", receipt, err)
	}
	request := stub.request
	if stub.calls != 1 || request.GetTaskRef() != cleanupTaskRef || request.GetAccountRef() != cleanupAccountRef ||
		request.GetLeaseGeneration() != 7 || request.GetCredential().GetSecretName() != cleanupCredential.SecretName ||
		request.GetCredential().GetSecretUid() != cleanupCredential.SecretUID ||
		request.GetCredential().GetSecretResourceVersion() != cleanupCredential.SecretResourceVersion ||
		request.GetCredential().GetContentSha256() != cleanupCredential.ContentSHA256 {
		t.Fatalf("cleanup request does not preserve exact task: %#v", request)
	}
}

// TestCleanupRecoveryPreservesCurrentClaimAndBindsEveryOriginField keeps the current claim separate from recovery origin
// and checks that changing each tested origin/current-claim field changes the deterministic request digest; it does not exercise signed transport.
// TestCleanupRecoveryPreservesCurrentClaimAndBindsEveryOriginField отделяет текущую заявку от происхождения восстановления
// и проверяет изменение детерминированного дайджеста при замене каждого проверяемого поля происхождения или текущей заявки; подписанный транспорт не задействован.
func TestCleanupRecoveryPreservesCurrentClaimAndBindsEveryOriginField(t *testing.T) {
	t.Parallel()
	stub := &providerCredentialClientStub{response: &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse{TerminalReceipt: cleanupReceipt}}
	origin := &entity.ProviderCleanupRecoveryIdentity{TaskRef: "pcct_61000000-0000-4000-8000-000000000099", Generation: 1, LegacyLastGeneration: 5}
	if _, err := cleanupClient(t, stub).CleanupProviderCredential(context.Background(), cleanupTaskRef, cleanupAccountRef, 7, cleanupCredential, origin); err != nil {
		t.Fatal(err)
	}
	request := stub.request
	if request.TaskRef != cleanupTaskRef || request.LeaseGeneration != 7 || request.RecoveryIdentity.TaskRef != origin.TaskRef || request.RecoveryIdentity.LeaseGeneration != 1 || request.RecoveryIdentity.LegacyLastGeneration != 5 {
		t.Fatal("recovery substituted current claim")
	}
	digest := func(message proto.Message) [32]byte {
		raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(raw)
	}
	baseline := digest(request)
	for _, mutate := range []func(*controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest){
		func(r *controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
			r.RecoveryIdentity.TaskRef = cleanupTaskRef
		},
		func(r *controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
			r.RecoveryIdentity.LeaseGeneration++
		},
		func(r *controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
			r.RecoveryIdentity.LegacyLastGeneration++
		},
		func(r *controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest) {
			r.LeaseGeneration++
		},
	} {
		changed := proto.Clone(request).(*controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest)
		mutate(changed)
		if digest(changed) == baseline {
			t.Fatal("canonical request digest omitted recovery/claim pins")
		}
	}
}

// TestCleanupProviderCredentialPropagatesRPCError verifies the upstream error is preserved after exactly one call,
// without an automatic retry that could repeat cleanup effects.
// TestCleanupProviderCredentialPropagatesRPCError проверяет сохранение ошибки вышестоящего сервиса после ровно одного вызова,
// без автоматического повтора, способного повторить последствия очистки.
func TestCleanupProviderCredentialPropagatesRPCError(t *testing.T) {
	t.Parallel()
	want := errors.New("cleanup unavailable")
	stub := &providerCredentialClientStub{err: want}
	_, err := cleanupClient(t, stub).CleanupProviderCredential(
		context.Background(), cleanupTaskRef, cleanupAccountRef, 1, cleanupCredential,
	)
	if !errors.Is(err, want) || stub.calls != 1 {
		t.Fatalf("RPC error was not propagated: calls=%d err=%v", stub.calls, err)
	}
}

// TestCleanupProviderCredentialRejectsInvalidRequestBeforeRPC mutates references, generation and descriptor fields
// and requires every invalid request to fail before reaching the stub.
// TestCleanupProviderCredentialRejectsInvalidRequestBeforeRPC изменяет ссылки, поколение и поля дескриптора
// и требует отказа для каждого некорректного запроса до обращения к подставному клиенту.
func TestCleanupProviderCredentialRejectsInvalidRequestBeforeRPC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		taskRef    string
		accountRef string
		generation int64
		credential entity.ProviderCredentialDescriptor
	}{
		{name: "task prefix", taskRef: "task_cleanup", accountRef: cleanupAccountRef, generation: 1, credential: cleanupCredential},
		{name: "task suffix", taskRef: "pcct_", accountRef: cleanupAccountRef, generation: 1, credential: cleanupCredential},
		{name: "task short suffix", taskRef: "pcct_short", accountRef: cleanupAccountRef, generation: 1, credential: cleanupCredential},
		{name: "account prefix", taskRef: cleanupTaskRef, accountRef: "account_cleanup", generation: 1, credential: cleanupCredential},
		{name: "account short suffix", taskRef: cleanupTaskRef, accountRef: "pacc_short", generation: 1, credential: cleanupCredential},
		{name: "account character", taskRef: cleanupTaskRef, accountRef: "pacc_cleanup/account", generation: 1, credential: cleanupCredential},
		{name: "lease generation", taskRef: cleanupTaskRef, accountRef: cleanupAccountRef, credential: cleanupCredential},
		{name: "secret name", taskRef: cleanupTaskRef, accountRef: cleanupAccountRef, generation: 1, credential: changedCredential(func(value *entity.ProviderCredentialDescriptor) { value.SecretName = "Invalid" })},
		{name: "secret UID", taskRef: cleanupTaskRef, accountRef: cleanupAccountRef, generation: 1, credential: changedCredential(func(value *entity.ProviderCredentialDescriptor) { value.SecretUID = "not-a-uuid" })},
		{name: "resource version", taskRef: cleanupTaskRef, accountRef: cleanupAccountRef, generation: 1, credential: changedCredential(func(value *entity.ProviderCredentialDescriptor) { value.SecretResourceVersion = " cleanup-7" })},
		{name: "digest length", taskRef: cleanupTaskRef, accountRef: cleanupAccountRef, generation: 1, credential: changedCredential(func(value *entity.ProviderCredentialDescriptor) { value.ContentSHA256 = "a" })},
		{name: "digest alphabet", taskRef: cleanupTaskRef, accountRef: cleanupAccountRef, generation: 1, credential: changedCredential(func(value *entity.ProviderCredentialDescriptor) { value.ContentSHA256 = strings.Repeat("A", 64) })},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stub := &providerCredentialClientStub{}
			_, err := cleanupClient(t, stub).CleanupProviderCredential(
				context.Background(), test.taskRef, test.accountRef, test.generation, test.credential,
			)
			if err == nil || stub.calls != 0 {
				t.Fatalf("invalid request reached RPC: calls=%d err=%v", stub.calls, err)
			}
		})
	}
}

// TestCleanupProviderCredentialRejectsInvalidTerminalReceipt checks empty, untrimmed, control-bearing and oversized receipts.
// Failure occurs after the RPC and does not imply rollback of any server effect.
// TestCleanupProviderCredentialRejectsInvalidTerminalReceipt проверяет пустые, необрезанные, содержащие управляющие символы и слишком длинные квитанции.
// Отказ происходит после RPC и не означает откат последствий на сервере.
func TestCleanupProviderCredentialRejectsInvalidTerminalReceipt(t *testing.T) {
	t.Parallel()
	for _, receipt := range []string{"", " receipt", "receipt\n", strings.Repeat("r", 513)} {
		receipt := receipt
		t.Run("invalid receipt", func(t *testing.T) {
			t.Parallel()
			stub := &providerCredentialClientStub{
				response: &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialResponse{
					TerminalReceipt: receipt,
				},
			}
			if _, err := cleanupClient(t, stub).CleanupProviderCredential(
				context.Background(), cleanupTaskRef, cleanupAccountRef, 1, cleanupCredential,
			); err == nil || stub.calls != 1 {
				t.Fatalf("invalid receipt was accepted: calls=%d", stub.calls)
			}
		})
	}
}

// changedCredential applies one mutation to a value copy of the synthetic descriptor and returns that copy,
// so table cases do not modify the shared baseline.
// changedCredential применяет одно изменение к копии синтетического дескриптора и возвращает её,
// поэтому строки таблицы не меняют общий исходный образец.
func changedCredential(change func(*entity.ProviderCredentialDescriptor)) entity.ProviderCredentialDescriptor {
	result := cleanupCredential
	change(&result)
	return result
}
