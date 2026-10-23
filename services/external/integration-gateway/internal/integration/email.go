package integration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/securefile"
)

// emailOrigin fixes the logical HTTPS origin used by mailbox commands independently of the selected internal transport profile.
// emailOrigin закрепляет логический HTTPS-источник почтовых команд независимо от выбранного профиля внутреннего транспорта.
const emailOrigin = "https://email-bridge.kodex-system.svc.cluster.local"

// emailExecutionBinding copies a valid lease timestamp and optional invocation or connection-test references into the bridge binding; serialization separately rejects ambiguous references.
// emailExecutionBinding переносит корректную временную метку аренды и необязательные ссылки вызова или проверки соединения в привязку моста; неоднозначные ссылки отдельно отклоняются при сериализации.
func emailExecutionBinding(invocation, test string, lease *cp.WorkLease) *api.ExecutionBinding {
	if lease == nil || lease.ExpiresAt == nil || lease.ExpiresAt.CheckValid() != nil {
		return nil
	}
	b := &api.ExecutionBinding{Lease: api.ExecutionLease{Ref: lease.Ref, Fence: lease.Fence, Generation: lease.Generation, ExpiresAt: lease.ExpiresAt.AsTime()}}
	if invocation != "" {
		b.InvocationRef = &invocation
	}
	if test != "" {
		b.ConnectionTestRef = &test
	}
	return b
}

// newEmailClient selects the explicit trusted-cluster transport or reads the full CA/client-keypair configuration for exact-SNI TLS 1.2 or newer; an entirely absent ordinary configuration disables this client.
// newEmailClient выбирает явный транспорт trusted-cluster либо читает полную конфигурацию CA и клиентской пары для TLS 1.2 или новее с точным SNI; полностью отсутствующая обычная конфигурация отключает клиент.
func newEmailClient(config Config) (*http.Client, error) {
	if config.RPCProfile != "" {
		if config.RPCProfile != transportprofile.TrustedCluster {
			return nil, errors.New("email RPC profile rejected")
		}
		return &http.Client{Timeout: config.Timeout,
			Transport: trustedEmailTransport{base: &http.Transport{MaxConnsPerHost: 8, MaxResponseHeaderBytes: 16384,
				ResponseHeaderTimeout: config.Timeout}},
			CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("email redirect forbidden") }}, nil
	}
	if config.EmailCAFile == "" && config.EmailCertificateFile == "" && config.EmailPrivateKeyFile == "" {
		return nil, nil
	}
	ca, e := securefile.Read(config.EmailCAFile, 1<<20)
	if e != nil {
		return nil, errors.New("email CA unavailable")
	}
	cert, e := securefile.Read(config.EmailCertificateFile, 1<<20)
	if e != nil {
		return nil, errors.New("email client certificate unavailable")
	}
	key, e := securefile.Read(config.EmailPrivateKeyFile, 1<<20)
	if e != nil {
		return nil, errors.New("email client key unavailable")
	}
	pair, e := tls.X509KeyPair(cert, key)
	if e != nil {
		return nil, errors.New("email client keypair invalid")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("email CA invalid")
	}
	return &http.Client{Timeout: config.Timeout, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "email-bridge.kodex-system.svc.cluster.local", RootCAs: roots, Certificates: []tls.Certificate{pair}}, TLSHandshakeTimeout: 5 * time.Second, MaxConnsPerHost: 8, MaxResponseHeaderBytes: 16384}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("email redirect forbidden") }}, nil
}

// testEmail substitutes the fixed health operation and executes it through the mailbox adapter using the supplied execution binding, without authorizing a mailbox mutation.
// testEmail подставляет закреплённую операцию готовности и выполняет её через почтовый адаптер с переданной привязкой выполнения, не разрешая изменение ящика.
func (adapter *Adapter) testEmail(ctx context.Context, request Request, configuration map[string]string) error {
	request.Operation = "email.delivery.health.read"
	capability, _ := adapter.definitions["email"].Capability(request.Operation)
	_, e := adapter.executeEmail(ctx, request, capability, configuration, []byte("{}"))
	return e
}

// executeEmail builds a typed command for the configured mailbox, requires a live execution lease and sends its fence to the bridge under that deadline; it never retries uncertain mutations or grants human approval itself.
// executeEmail создаёт типизированную команду заданного ящика, требует действующую аренду выполнения и передаёт её fence мосту в пределах срока; неопределённые изменения не повторяет и человеческое подтверждение самостоятельно не выдаёт.
func (adapter *Adapter) executeEmail(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, input []byte) (Result, error) {
	if request.Credential != nil || configuration["base_url"] != emailOrigin || adapter.emailHTTPClient == nil {
		return Result{}, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	command, e := api.CommandForIntegration(request.Operation, configuration["mailbox_id"], configuration["from_address"], request.EffectKey, input)
	if e != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	// Connection credential не доказывает claim. Fence проверяет CP по exact binding.
	execution, e := api.ExecutionHeaderValue(request.EmailExecution)
	if e != nil || !request.EmailExecution.Lease.ExpiresAt.After(time.Now()) {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if request.EmailExecution.ConnectionTestRef != nil && command.Operation != api.OperationHealth {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	// HTTP не должен пережить срок authority даже при более длинном caller context.
	ctx, cancel := context.WithDeadline(ctx, request.EmailExecution.Lease.ExpiresAt)
	defer cancel()
	client, e := api.NewClient(emailOrigin, api.WithHTTPClient(adapter.emailHTTPClient))
	if e != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	response, e := client.ExecuteMailboxOperation(ctx, command, func(_ context.Context, r *http.Request) error {
		r.Header.Set("Authorization", "Bearer "+request.EmailExecution.Lease.Fence)
		r.Header.Set(api.ExecutionHeader, execution)
		r.GetBody = nil
		return nil
	})
	mutation := capability.Risk != "READ"
	if e != nil {
		if mutation {
			return Result{}, &UnknownOutcomeError{}
		}
		return Result{}, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(response.Body, 65537))
	if e != nil || len(raw) > 65536 {
		if mutation {
			return Result{}, &UnknownOutcomeError{}
		}
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if mutation && response.StatusCode >= 500 {
			return Result{}, &UnknownOutcomeError{}
		}
		return Result{}, statusError(response.StatusCode)
	}
	var result api.Result
	if api.Decode(raw, &result) != nil {
		if mutation {
			return Result{}, &UnknownOutcomeError{}
		}
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	if mutation {
		if result.Status == "unknown" {
			return Result{}, &UnknownOutcomeError{}
		}
		if result.Status == "failed" {
			return Result{}, &SafeError{Code: "INTEGRATION_PROVIDER_REJECTED"}
		}
		expected := "accepted"
		if command.Operation == api.OperationDelete || command.Operation == api.OperationDraftDelete {
			expected = "deleted"
		}
		if result.Status != expected || result.MessageId == "" {
			return Result{}, &UnknownOutcomeError{}
		}
		return providerResult(request, "email-message:"+result.MessageId, map[string]any{"message_id": result.MessageId, "status": result.Status, "result_json": string(raw)})
	}
	if command.Operation == api.OperationHealth {
		// Старый bridge мог вернуть только статус без protocol report.
		if result.ProtocolReadiness == nil {
			if result.Status != "ready" {
				return Result{}, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
			}
			return providerResult(request, "email-bridge:health", map[string]any{"status": result.Status, "result_json": string(raw)})
		}
		summary, err := api.HealthSummary(result.Status, result.ProtocolReadiness)
		if err != nil {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		if result.Status != "ready" {
			return Result{}, &SafeError{Code: api.HealthNotReadyCode, HealthSummary: summary}
		}
		output, err := providerResult(request, "email-bridge:health", map[string]any{"status": result.Status, "result_json": string(raw)})
		output.HealthSummary = summary
		return output, err
	}
	if command.Operation == api.OperationReceipt {
		if result.MessageId == "" || (command.ReceiptId != "" && result.MessageId != command.ReceiptId) || !strings.Contains("|accepted|failed|unknown|deleted|", "|"+result.Status+"|") {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return providerResult(request, "email-message:"+result.MessageId, map[string]any{"message_id": result.MessageId, "status": result.Status, "result_json": string(raw)})
	}
	if result.Status != "ok" || !validEmailReadback(command, result) {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	encoded, e := json.Marshal(result)
	if e != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return providerResult(request, "email-mailbox:"+command.MailboxId, map[string]any{"result_json": string(encoded)})
}

// validEmailReadback checks the requested UID and any supplied folder or UIDVALIDITY for message operations, or the thread ID for thread reads; other operation shapes are validated elsewhere.
// validEmailReadback проверяет запрошенный UID и переданные папку либо UIDVALIDITY для операций с письмом или идентификатор цепочки для её чтения; остальные формы операций проверяются отдельно.
func validEmailReadback(command api.Command, result api.Result) bool {
	switch command.Operation {
	case api.OperationFetch, api.OperationDownload, api.OperationAttachments:
		return result.Uid == command.Uid &&
			(command.UidValidity == 0 || result.UidValidity == command.UidValidity) &&
			(command.Folder == "" || result.Folder == command.Folder)
	case api.OperationThread:
		return result.ThreadId == command.ThreadId
	default:
		return true
	}
}
