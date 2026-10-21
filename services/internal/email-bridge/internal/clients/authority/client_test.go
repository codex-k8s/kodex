package authority

import (
	"context"
	"errors"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeRuntime records authorization requests and synthesizes matching scope responses or injected RPC failures without a real owner server.
// fakeRuntime записывает запросы авторизации и создаёт совпадающие scope-ответы либо внедрённые RPC-ошибки без настоящего сервера владельца.
type fakeRuntime struct {
	cp.RuntimeWorkServiceClient
	request *cp.ResolveEmailAuthorizationRequest
	mutate  func(*cp.ResolveEmailAuthorizationResponse)
	failure error
}

// ResolveEmailAuthorization clones the request, builds structurally matching scopes and applies the scenario mutation before returning.
// Its synthetic response tests adapter shape/binding checks, not domain eligibility or transport authentication.
// ResolveEmailAuthorization копирует запрос, создаёт структурно совпадающие scope и применяет изменение сценария перед возвратом.
// Синтетический ответ проверяет форму и привязки адаптера, не доменный допуск или транспортную аутентификацию.
func (f *fakeRuntime) ResolveEmailAuthorization(_ context.Context, r *cp.ResolveEmailAuthorizationRequest, _ ...grpc.CallOption) (*cp.ResolveEmailAuthorizationResponse, error) {
	f.request = proto.Clone(r).(*cp.ResolveEmailAuthorizationRequest)
	if f.failure != nil {
		return nil, f.failure
	}
	scope := &cp.EmailAuthorizationScope{MailboxRef: r.MailboxRef, Sender: r.Sender, Operations: []cp.EmailOperation{r.Operation}, Folders: []string{r.Folder}}
	result := &cp.ResolveEmailAuthorizationResponse{Binding: proto.Clone(r.Binding).(*cp.EmailExecutionBinding), Operation: r.Operation, Policy: cp.EmailApprovalPolicy_EMAIL_APPROVAL_POLICY_ALLOW, ExpiresAt: timestamppb.New(time.Now().Add(20 * time.Second)), UserScope: scope, AgentScope: scope, ConnectionScope: scope, ResourceScope: scope}
	if r.Binding.GetConnectionTestRef() != "" {
		result.AgentScope = nil
	}
	if f.mutate != nil {
		f.mutate(result)
	}
	return result, nil
}

// fixtureRequest provides a future-dated synthetic invocation lease whose token matches its fence for isolated mapping tests.
// fixtureRequest задаёт синтетический invocation lease с будущим сроком и token, совпадающим с fence, для изолированных тестов преобразования.
func fixtureRequest() api.AuthorizationRequest {
	ref := "inv_fixture01"
	return api.AuthorizationRequest{InvocationToken: "fence", Operation: api.OperationHealth, MailboxId: "mailbox", ExecutionBinding: &api.ExecutionBinding{InvocationRef: &ref, Lease: api.ExecutionLease{Ref: "lease", Fence: "fence", Generation: 1, ExpiresAt: time.Now().Add(time.Minute)}}}
}

// TestExactEmailOperationMapping checks all 21 supported non-deprecated mail operations retain exact Proto operation and execution binding.
// TestExactEmailOperationMapping проверяет сохранение точных Proto operation и execution binding для всех 21 поддержанной неустаревшей операции почты.
func TestExactEmailOperationMapping(t *testing.T) {
	count := 0
	for _, op := range api.Operations() {
		if op == api.OperationMark {
			continue
		}
		input := fixtureRequest()
		input.Operation = op
		f := &fakeRuntime{}
		out, err := (&Client{API: f}).Resolve(t.Context(), input)
		if err != nil || out.Operation != op || f.request.GetOperation() == 0 || !proto.Equal(f.request.Binding, Binding(input.ExecutionBinding)) {
			t.Fatalf("operation %s mapping failed: %v", op, err)
		}
		count++
	}
	if count != 21 {
		t.Fatalf("operation registry count=%d", count)
	}
}

// TestEmailBindingAndSourceFailClosed rejects missing, expired, dual or mismatched lineage and malformed owner responses while allowing agentless health tests.
// The final cases verify denied versus unavailable RPC classification without an actual owner connection.
// TestEmailBindingAndSourceFailClosed отвергает отсутствующее, истёкшее, двойное или несовпавшее происхождение и неверные ответы, допуская health без агента.
// Финальные сценарии проверяют различие denied и unavailable RPC без настоящего соединения с владельцем.
func TestEmailBindingAndSourceFailClosed(t *testing.T) {
	for _, name := range []string{"missing", "wrong-fence", "expired", "dual-source", "test-mutation", "response-generation", "response-source", "response-expiry", "unknown-policy", "unknown-scope", "test-agent"} {
		t.Run(name, func(t *testing.T) {
			input := fixtureRequest()
			f := &fakeRuntime{}
			testRef := "test_fixture01"
			switch name {
			case "missing":
				input.ExecutionBinding = nil
			case "wrong-fence":
				input.InvocationToken = "other"
			case "expired":
				input.ExecutionBinding.Lease.ExpiresAt = time.Now().Add(-time.Second)
			case "dual-source":
				input.ExecutionBinding.ConnectionTestRef = &testRef
			case "test-mutation":
				input.ExecutionBinding.InvocationRef = nil
				input.ExecutionBinding.ConnectionTestRef = &testRef
				input.Operation = api.OperationSend
			case "response-generation":
				f.mutate = func(r *cp.ResolveEmailAuthorizationResponse) { r.Binding.Lease.Generation++ }
			case "response-source":
				f.mutate = func(r *cp.ResolveEmailAuthorizationResponse) {
					r.Binding.Source = &cp.EmailExecutionBinding_InvocationRef{InvocationRef: "other"}
				}
			case "response-expiry":
				f.mutate = func(r *cp.ResolveEmailAuthorizationResponse) {
					r.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
				}
			case "unknown-policy":
				f.mutate = func(r *cp.ResolveEmailAuthorizationResponse) { r.Policy = 999 }
			case "unknown-scope":
				f.mutate = func(r *cp.ResolveEmailAuthorizationResponse) { r.UserScope.Operations = []cp.EmailOperation{999} }
			case "test-agent":
				input.ExecutionBinding.InvocationRef = nil
				input.ExecutionBinding.ConnectionTestRef = &testRef
				f.mutate = func(r *cp.ResolveEmailAuthorizationResponse) { r.AgentRef = "invented" }
			}
			if _, err := (&Client{API: f}).Resolve(t.Context(), input); err == nil {
				t.Fatal("invalid binding accepted")
			}
		})
	}
	input := fixtureRequest()
	ref := "test_fixture01"
	input.ExecutionBinding.InvocationRef = nil
	input.ExecutionBinding.ConnectionTestRef = &ref
	if _, err := (&Client{API: &fakeRuntime{}}).Resolve(t.Context(), input); err != nil {
		t.Fatal("agentless health test rejected")
	}
	for _, code := range []codes.Code{codes.PermissionDenied, codes.Unauthenticated, codes.Unimplemented, codes.Unavailable} {
		_, err := (&Client{API: &fakeRuntime{failure: status.Error(code, "fixture")}}).Resolve(t.Context(), fixtureRequest())
		want := errs.Unavailable
		if code == codes.PermissionDenied || code == codes.Unauthenticated {
			want = errs.Denied
		}
		if !errors.Is(err, want) {
			t.Fatal("authority failure was reclassified")
		}
	}
}
