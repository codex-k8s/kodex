package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	authorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// warmDiagnosticSentinel is synthetic private-looking text placed in status messages, details and cached tickets to detect disclosure.
// It contains no real JWS, credential reference or secret.
// warmDiagnosticSentinel является синтетическим текстом, похожим на частные данные, в сообщениях статуса, деталях и кэше билетов для обнаружения раскрытия.
// Настоящих JWS, ссылок учётных данных и секретов в нём нет.
const warmDiagnosticSentinel = "PRIVATE-JWS-credential-ref-sentinel"

// warmDiagnosticOwner returns configured reconciliation/report responses or failures and counts those local calls.
// Only the exercised RPC methods are replaced; no owner service or provider is contacted.
// warmDiagnosticOwner возвращает заданные ответы или ошибки согласования/отчёта и считает локальные вызовы.
// Заменены только вызываемые методы RPC; сервис владельца и провайдер не вызываются.
type warmDiagnosticOwner struct {
	controlplanev1.RuntimeWorkServiceClient
	response *controlplanev1.ReconcileWarmRuntimeResponse
	err      error
	calls    int
}

// warmDiagnosticDeactivator records deactivation requests and returns the configured error without removing an actual workload.
// warmDiagnosticDeactivator записывает запросы отключения и возвращает настроенную ошибку без удаления настоящей нагрузки.
type warmDiagnosticDeactivator struct {
	err   error
	calls int
}

// DeactivateWarm increments the synthetic deactivation count and preserves the configured error as returned.
// It neither alters Kubernetes objects nor revokes a credential.
// DeactivateWarm увеличивает синтетический счётчик отключений и возвращает настроенную ошибку без подмены.
// Объекты Kubernetes не меняются, учётные данные не отзываются.
func (deactivator *warmDiagnosticDeactivator) DeactivateWarm(context.Context) error {
	deactivator.calls++
	return deactivator.err
}

// ReconcileWarmRuntime counts the call and panics if the request lacks a deadline or the expected workload instance.
// It then returns the configured synthetic response/error without server-side reconciliation.
// ReconcileWarmRuntime считает вызов и вызывает панику при отсутствии срока запроса или ожидаемого экземпляра нагрузки.
// Затем возвращается настроенный синтетический ответ/ошибка без серверного согласования.
func (owner *warmDiagnosticOwner) ReconcileWarmRuntime(ctx context.Context, request *controlplanev1.ReconcileWarmRuntimeRequest, _ ...grpc.CallOption) (*controlplanev1.ReconcileWarmRuntimeResponse, error) {
	owner.calls++
	if _, ok := ctx.Deadline(); !ok || request.GetWorkloadInstance() != "test-pod" {
		panic("warm request contract changed")
	}
	return owner.response, owner.err
}

// ReportWarmRuntime counts the call and checks a request deadline, workload instance, exact revision and READY state.
// The empty response or configured error is local test behavior, not a persisted owner observation.
// ReportWarmRuntime считает вызов и проверяет срок запроса, экземпляр нагрузки, точную ревизию и состояние READY.
// Пустой ответ или настроенная ошибка являются локальным поведением теста, а не сохранённым наблюдением владельца.
func (owner *warmDiagnosticOwner) ReportWarmRuntime(ctx context.Context, request *controlplanev1.ReportWarmRuntimeRequest, _ ...grpc.CallOption) (*controlplanev1.ReportWarmRuntimeResponse, error) {
	owner.calls++
	if _, ok := ctx.Deadline(); !ok || request.GetWorkloadInstance() != "test-pod" || request.GetRuntimeRevision() != "test-revision" || request.GetState() != controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY {
		panic("warm report contract changed")
	}
	return &controlplanev1.ReportWarmRuntimeResponse{}, owner.err
}

// warmDiagnosticRuntime builds only the stubs, readiness and request settings needed by the diagnostic paths.
// It intentionally has no real workload manager or provider session and starts no background work.
// warmDiagnosticRuntime создаёт только заглушки, готовность и настройки запроса для диагностических путей.
// Настоящие менеджер нагрузок и сессия провайдера намеренно отсутствуют; фоновая работа не запускается.
func warmDiagnosticRuntime(owner *warmDiagnosticOwner) *runtime {
	return &runtime{control: owner, warmDeactivator: &warmDiagnosticDeactivator{}, assistant: serviceruntime.NewReadiness(), config: Config{PodUID: "test-pod", RequestTimeout: time.Second}}
}

// assertWarmDiagnostic requires an exact error string with no unwrapped cause, then writes one synthetic JSON warning and rejects the private sentinel.
// The helper checks this diagnostic boundary rather than general-purpose redaction of arbitrary data.
// assertWarmDiagnostic требует точную строку ошибки без разворачиваемой причины, затем пишет одно синтетическое JSON-предупреждение и запрещает частный маркер.
// Проверяется эта диагностическая граница, а не универсальная очистка произвольных данных.
func assertWarmDiagnostic(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want || errors.Unwrap(err) != nil {
		t.Fatal("unexpected warm diagnostic or retained upstream cause")
	}
	var output bytes.Buffer
	slog.New(slog.NewJSONHandler(&output, nil)).Warn("system assistant warm runtime reconciliation failed", "error", err)
	if strings.Contains(output.String(), warmDiagnosticSentinel) {
		t.Fatal("upstream data escaped in warm log")
	}
}

// TestWarmRPCDiagnosticsDropMessagesAndDetails injects private status messages, typed/unknown protobuf details and wrapped causes across canonical and out-of-range codes.
// Reconciliation/report/claim diagnostics must retain only normalized codes without repeating owner calls or leaking the sentinel.
// TestWarmRPCDiagnosticsDropMessagesAndDetails задаёт частные сообщения статуса, типизированные/неизвестные детали protobuf и обёрнутые причины для штатных и внештатных кодов.
// Диагностика согласования/отчёта/заявки должна сохранять только нормализованные коды без повторных вызовов владельца или утечки маркера.
func TestWarmRPCDiagnosticsDropMessagesAndDetails(t *testing.T) {
	for _, code := range []codes.Code{codes.Canceled, codes.Unknown, codes.InvalidArgument, codes.DeadlineExceeded, codes.NotFound, codes.AlreadyExists, codes.PermissionDenied, codes.ResourceExhausted, codes.FailedPrecondition, codes.Aborted, codes.OutOfRange, codes.Unimplemented, codes.Internal, codes.Unavailable, codes.DataLoss, codes.Unauthenticated, codes.Code(999)} {
		t.Run(code.String(), func(t *testing.T) {
			detail := &authorityv1.AuthorizationErrorDetail{CorrelationId: warmDiagnosticSentinel}
			detail.ProtoReflect().SetUnknown(append([]byte{0xa2, 0x06, byte(len(warmDiagnosticSentinel))}, []byte(warmDiagnosticSentinel)...))
			st, err := status.New(code, warmDiagnosticSentinel).WithDetails(detail)
			if err != nil {
				t.Fatal("build synthetic status")
			}
			owner := &warmDiagnosticOwner{err: fmt.Errorf("%s: %w", warmDiagnosticSentinel, st.Err())}
			runtime := warmDiagnosticRuntime(owner)
			wantCode := code
			if code > codes.Unauthenticated {
				wantCode = codes.Unknown
			}
			assertWarmDiagnostic(t, runtime.reconcileWarm(t.Context()), warmReconcileRPCFailure+": grpc_code="+wantCode.String())
			assertWarmDiagnostic(t, runtime.reportWarm(t.Context(), "test-revision", controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY, ""), warmReportRPCFailure+": grpc_code="+wantCode.String())
			assertWarmDiagnostic(t, boundedRPCFailure(runtimeClaimRPCFailure, owner.err), runtimeClaimRPCFailure+": grpc_code="+wantCode.String())
			if owner.calls != 2 {
				t.Fatal("diagnostics repeated owner effect")
			}
		})
	}
	owner := &warmDiagnosticOwner{err: errors.New(warmDiagnosticSentinel)}
	assertWarmDiagnostic(t, warmDiagnosticRuntime(owner).reconcileWarm(t.Context()), warmReconcileRPCFailure+": grpc_code=Unknown")
}

// TestWarmMissingDesiredRevisionIsDistinctFromRPCFailure covers nil/empty reconciliation responses and failure still requiring materialization.
// Missing revision must remain its own diagnostic without repeated reconciliation, while a successful synthetic report stays successful.
// TestWarmMissingDesiredRevisionIsDistinctFromRPCFailure охватывает nil/пустые ответы согласования и ошибку с необходимым размещением.
// Отсутствующая ревизия должна сохранять отдельную диагностику без повторного согласования, а успешный синтетический отчёт — оставаться успешным.
func TestWarmMissingDesiredRevisionIsDistinctFromRPCFailure(t *testing.T) {
	for _, response := range []*controlplanev1.ReconcileWarmRuntimeResponse{
		nil,
		{},
		{
			Assistant:               &controlplanev1.SystemAssistant{RuntimeState: controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_FAILED},
			MaterializationRequired: true,
		},
	} {
		owner := &warmDiagnosticOwner{response: response}
		runtime := warmDiagnosticRuntime(owner)
		assertWarmDiagnostic(t, runtime.reconcileWarm(t.Context()), warmDesiredRevisionMissing)
		if owner.calls != 1 {
			t.Fatal("missing revision repeated owner effect")
		}
		if err := runtime.reportWarm(t.Context(), "test-revision", controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY, ""); err != nil {
			t.Fatal("successful report changed")
		}
	}
}

// TestWarmUnavailableWithoutDesiredRevisionIsIdleAndRevokesLocalRuntime checks the explicit idle/unconfigured-provider response clears cached compatibility/ticket and marks readiness unavailable.
// It verifies one synthetic deactivation and propagation of a later deactivation error, not deletion of a real Pod.
// TestWarmUnavailableWithoutDesiredRevisionIsIdleAndRevokesLocalRuntime проверяет очистку кэша совместимости/билета и неготовность при явном ответе простоя с ненастроенным провайдером.
// Проверяются одно синтетическое отключение и передача последующей ошибки отключения, а не удаление настоящего Pod.
func TestWarmUnavailableWithoutDesiredRevisionIsIdleAndRevokesLocalRuntime(t *testing.T) {
	owner := &warmDiagnosticOwner{response: &controlplanev1.ReconcileWarmRuntimeResponse{
		Assistant: &controlplanev1.SystemAssistant{RuntimeState: controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_FAILED},
	}}
	deactivator := &warmDiagnosticDeactivator{}
	runtime := warmDiagnosticRuntime(owner)
	runtime.warmDeactivator = deactivator
	runtime.warmCompatibility = warmDiagnosticSentinel
	runtime.warmTicket = warmDiagnosticSentinel
	if err := runtime.reconcileWarm(t.Context()); err != nil {
		t.Fatalf("provider-free idle returned an error: %v", err)
	}
	if deactivator.calls != 1 || runtime.warmCompatibility != "" || runtime.warmTicket != "" {
		t.Fatalf("provider-free idle retained local warm authority: calls=%d compatibility=%q ticket=%q", deactivator.calls, runtime.warmCompatibility, runtime.warmTicket)
	}
	if ready, reason := runtime.assistant.Ready(); ready || reason != "assistant_runtime_provider_unconfigured" {
		t.Fatalf("provider-free readiness = %t/%q", ready, reason)
	}
	deactivator.err = errors.New("delete warm runtime Pod")
	assertWarmDiagnostic(t, runtime.reconcileWarm(t.Context()), "delete warm runtime Pod")
}

// warmDiagnosticAuthority supplies a fixed operation and synthetic proof/issuer failures for interceptor diagnostic tests.
// It is not a live authority client and does not grant usable application permissions.
// warmDiagnosticAuthority задаёт фиксированную операцию и синтетические ошибки доказательства/издателя для тестов диагностики перехватчика.
// Это не живой клиент полномочий; пригодные прикладные разрешения не выдаются.
type warmDiagnosticAuthority struct {
	authorityv1.AuthorizationIssuerServiceClient
	stage authorityclient.DiagnosticStage
	cause error
}

// OperationID returns the fixed test operation for every method so the fixture reaches the selected diagnostic stage.
// It deliberately performs no real operation-registry authorization.
// OperationID возвращает фиксированную тестовую операцию для любого метода, чтобы образец дошёл до выбранной стадии диагностики.
// Настоящая авторизация реестра операций намеренно не выполняется.
func (authority warmDiagnosticAuthority) OperationID(string) (string, bool) {
	return "test-operation", true
}

// AuthorityProof returns a closed synthetic provider failure except when the test targets context issuance.
// That case returns placeholder proof/correlation text solely to reach the failing issuer stub.
// AuthorityProof возвращает закрытую синтетическую ошибку поставщика, кроме проверки стадии выдачи контекста.
// В этом случае возвращаются заполнители доказательства/корреляции только для достижения ошибающейся заглушки издателя.
func (authority warmDiagnosticAuthority) AuthorityProof(context.Context, string, string) (string, string, error) {
	if authority.stage != authorityclient.StageContextIssue {
		return "", warmDiagnosticSentinel, authorityclient.NewProofFailure(authority.stage, authority.cause)
	}
	return "synthetic-proof", warmDiagnosticSentinel, nil
}

// IssueAuthorizationContext always returns the configured issuer failure with no authorization response.
// It creates no real context, lease or grant.
// IssueAuthorizationContext всегда возвращает настроенную ошибку издателя без ответа авторизации.
// Настоящие контекст, аренда и разрешение не создаются.
func (authority warmDiagnosticAuthority) IssueAuthorizationContext(context.Context, *authorityv1.IssueAuthorizationContextRequest, ...grpc.CallOption) (*authorityv1.IssueAuthorizationContextResponse, error) {
	return nil, authority.cause
}

// TestWarmLocalAuthorityDiagnosticPreservesOnlyClosedStages drives six synthetic proof/issuer failure stages through the real local client interceptor.
// The downstream invoker must stay unreachable; warm diagnostics retain only the normalized local stage/code without wrapped private causes or repeated owner effects.
// TestWarmLocalAuthorityDiagnosticPreservesOnlyClosedStages проводит шесть синтетических стадий ошибки доказательства/издателя через настоящий локальный перехватчик клиента.
// Нижележащий вызов должен оставаться недостижимым; диагностика прогрева сохраняет только нормализованные локальные стадию/код без обёрнутых частных причин или повторных действий владельца.
func TestWarmLocalAuthorityDiagnosticPreservesOnlyClosedStages(t *testing.T) {
	for _, stage := range []authorityclient.DiagnosticStage{authorityclient.StageGrantMissing, authorityclient.StageGrantRead, authorityclient.StageProofOperation, authorityclient.StageProofResolve, authorityclient.StageProofResponse, authorityclient.StageContextIssue} {
		t.Run(string(stage), func(t *testing.T) {
			st, err := status.New(codes.PermissionDenied, warmDiagnosticSentinel).WithDetails(&authorityv1.AuthorizationErrorDetail{Reason: authorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_PERMISSION_MISMATCH, Stage: authorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_POLICY, CorrelationId: warmDiagnosticSentinel})
			if err != nil {
				t.Fatal("build synthetic authority status")
			}
			authority := warmDiagnosticAuthority{stage: stage, cause: st.Err()}
			err = authorityclient.IssuerUnaryClientInterceptor(authority, authority, authority)(t.Context(), "/test", &controlplanev1.ReconcileWarmRuntimeRequest{}, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
				t.Fatal("downstream effect reached after authority rejection")
				return nil
			})
			var local *authorityclient.LocalAuthorityError
			if !errors.As(err, &local) || local.Diagnostic().Stage != stage {
				t.Fatal("local authority fixture failed")
			}
			owner := &warmDiagnosticOwner{err: fmt.Errorf("%s: %w", warmDiagnosticSentinel, err)}
			runtime := warmDiagnosticRuntime(owner)
			want := ": grpc_code=Unauthenticated [" + local.Diagnostic().String() + "]"
			assertWarmDiagnostic(t, runtime.reconcileWarm(t.Context()), warmReconcileRPCFailure+want)
			assertWarmDiagnostic(t, runtime.reportWarm(t.Context(), "test-revision", controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY, ""), warmReportRPCFailure+want)
			if owner.calls != 2 {
				t.Fatal("diagnostics repeated owner effect")
			}
		})
	}
}
