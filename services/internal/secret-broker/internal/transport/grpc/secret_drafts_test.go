package grpc

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// draftRPCFixtureGrant is an opaque synthetic input, never a valid authority token.
// draftRPCFixtureGrant — непрозрачный synthetic input, не действующий authority token.
const draftRPCFixtureGrant = "synthetic-draft-grant"

// draftRPCFixturePlaintext is disposable test data used to observe deferred input clearing.
// draftRPCFixturePlaintext — disposable test data для проверки отложенного обнуления входа.
const draftRPCFixturePlaintext = "synthetic-draft-value"

// draftRPCFixture records exact direct-handler forwarding and injects metadata or a failure.
// It replaces domain authority/storage and therefore proves neither grant validation nor persistence.
// draftRPCFixture фиксирует точную передачу direct-handler аргументов и подставляет metadata/ошибку.
// Она заменяет domain authority/storage и не доказывает проверку grants или сохранение состояния.
type draftRPCFixture struct {
	t         *testing.T
	context   context.Context
	operation value.DraftOperation
	result    value.DraftResult
	failure   error
	calls     int
}

// Check satisfies the unused fixture readiness port without consulting a real dependency.
// Check реализует неиспользуемый fixture readiness port без обращения к реальной зависимости.
func (*draftRPCFixture) Check(context.Context) error { return nil }

// Execute checks the unchanged context, closed operation, opaque grant and SAVE-only plaintext
// before its caller's defer clears that slice; it performs no external effect.
// Execute проверяет неизменные context, закрытую operation, opaque grant и SAVE-only plaintext
// до обнуления slice отложенной операцией caller; внешних эффектов она не выполняет.
func (fixture *draftRPCFixture) Execute(ctx context.Context, operation value.DraftOperation, grant string, plaintext []byte) (value.DraftResult, error) {
	fixture.calls++
	if ctx != fixture.context || operation != fixture.operation || grant != draftRPCFixtureGrant {
		fixture.t.Fatal("draft handler changed operation forwarding")
	}
	if operation == value.DraftSave {
		if string(plaintext) != draftRPCFixturePlaintext {
			fixture.t.Fatal("draft handler cleared plaintext before execution")
		}
	} else if plaintext != nil {
		fixture.t.Fatal("non-save draft operation received plaintext")
	}
	return fixture.result, fixture.failure
}

// draftRPCResult creates fresh scoped metadata for a synthetic operation, not an authorized domain result.
// draftRPCResult создаёт отдельные scoped metadata synthetic operation, не авторизованный domain result.
func draftRPCResult(operation value.DraftOperation) value.DraftResult {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	state := map[value.DraftOperation]string{
		value.DraftSave: "DRAFT", value.DraftValidate: "VALID", value.DraftPublish: "PUBLISHED", value.DraftDiscard: "DISCARDED",
	}[operation]
	result := value.DraftResult{Draft: value.SecretDraft{ScopeKind: runtimesecret.ScopeOrganization,
		OrganizationRef: "org_fixture", Ref: "draft_fixture", SecretRef: "sec_fixture", Name: "SYNTHETIC_KEY",
		Version: 3, Generation: 2, SecretVersion: 2, ValueType: "STRING", State: state,
		CreatedAt: now, UpdatedAt: now.Add(time.Second), ExpiresAt: now.Add(time.Hour)}}
	if operation == value.DraftPublish {
		result.Draft.PublishedRevision = 1
		result.Secret = &value.PublishedSecret{ScopeKind: result.Draft.ScopeKind, OrganizationRef: result.Draft.OrganizationRef,
			Ref: result.Draft.SecretRef, Name: result.Draft.Name, Version: 2, Revision: 1, ValueType: "STRING", Status: "ACTIVE",
			CreatedAt: now, UpdatedAt: now.Add(time.Second)}
	}
	return result
}

// TestDraftRPCResultConversionPreservesOperationAndFailurePaths checks each direct RPC wrapper,
// exact command arguments, closed failures, publish-only metadata and SAVE clearing after all outcomes.
// TestDraftRPCResultConversionPreservesOperationAndFailurePaths проверяет каждый direct RPC wrapper,
// точные command arguments, закрытые ошибки, publish-only metadata и SAVE clearing при любом исходе.
func TestDraftRPCResultConversionPreservesOperationAndFailurePaths(t *testing.T) {
	for _, operation := range []struct {
		kind value.DraftOperation
		call func(context.Context, *Server, []byte) (*secretbrokerv1.RuntimeSecretDraftMetadata, *secretbrokerv1.RuntimeSecretMetadata, error)
	}{
		{value.DraftSave, func(ctx context.Context, server *Server, plaintext []byte) (*secretbrokerv1.RuntimeSecretDraftMetadata, *secretbrokerv1.RuntimeSecretMetadata, error) {
			response, err := server.SaveSecretDraft(ctx, &secretbrokerv1.SaveSecretDraftRequest{OperationGrant: draftRPCFixtureGrant, Value: plaintext})
			return response.GetDraft(), nil, err
		}},
		{value.DraftValidate, func(ctx context.Context, server *Server, _ []byte) (*secretbrokerv1.RuntimeSecretDraftMetadata, *secretbrokerv1.RuntimeSecretMetadata, error) {
			response, err := server.ValidateSecretDraft(ctx, &secretbrokerv1.ValidateSecretDraftRequest{OperationGrant: draftRPCFixtureGrant})
			return response.GetDraft(), nil, err
		}},
		{value.DraftPublish, func(ctx context.Context, server *Server, _ []byte) (*secretbrokerv1.RuntimeSecretDraftMetadata, *secretbrokerv1.RuntimeSecretMetadata, error) {
			response, err := server.PublishSecretDraft(ctx, &secretbrokerv1.PublishSecretDraftRequest{OperationGrant: draftRPCFixtureGrant})
			return response.GetDraft(), response.GetSecret(), err
		}},
		{value.DraftDiscard, func(ctx context.Context, server *Server, _ []byte) (*secretbrokerv1.RuntimeSecretDraftMetadata, *secretbrokerv1.RuntimeSecretMetadata, error) {
			response, err := server.DiscardSecretDraft(ctx, &secretbrokerv1.DiscardSecretDraftRequest{OperationGrant: draftRPCFixtureGrant})
			return response.GetDraft(), nil, err
		}},
	} {
		for _, scenario := range []string{"success", "command failure", "invalid draft", "missing published secret"} {
			if scenario == "missing published secret" && operation.kind != value.DraftPublish {
				continue
			}
			t.Run(string(operation.kind)+"/"+scenario, func(t *testing.T) {
				fixture := &draftRPCFixture{t: t, context: t.Context(), operation: operation.kind, result: draftRPCResult(operation.kind)}
				wantCode := codes.OK
				switch scenario {
				case "command failure":
					fixture.failure, wantCode = status.Error(codes.PermissionDenied, "PRIVATE_DRAFT_DIAGNOSTIC"), codes.PermissionDenied
				case "invalid draft":
					fixture.result.Draft.Ref, wantCode = "", codes.Unavailable
				case "missing published secret":
					fixture.result.Secret, wantCode = nil, codes.Unavailable
				}
				plaintext := []byte(draftRPCFixturePlaintext)
				draft, published, err := operation.call(fixture.context, &Server{drafts: fixture}, plaintext)
				if fixture.calls != 1 || status.Code(err) != wantCode {
					t.Fatal("draft RPC changed execution count or error classification")
				}
				if operation.kind == value.DraftSave && !bytes.Equal(plaintext, make([]byte, len(plaintext))) {
					t.Fatal("draft RPC retained request plaintext")
				}
				if wantCode != codes.OK {
					if draft != nil || published != nil || status.Convert(err).Message() != "secret draft operation failed" || strings.Contains(err.Error(), "PRIVATE_") {
						t.Fatal("draft RPC exposed a partial response or private diagnostic")
					}
					return
				}
				if draft.GetRef() != fixture.result.Draft.Ref || draft.GetState().String() != "RUNTIME_SECRET_DRAFT_STATE_"+fixture.result.Draft.State ||
					draft.GetOrganizationRef() != fixture.result.Draft.OrganizationRef || draft.GetProjectRef() != "" || draft.GetVersion() != 3 ||
					(published != nil) != (operation.kind == value.DraftPublish) {
					t.Fatal("draft RPC changed successful metadata")
				}
				if published != nil && (published.GetSecretRef() != fixture.result.Secret.Ref || published.GetRevision() != 1) {
					t.Fatal("publish RPC lost its published metadata")
				}
			})
		}
	}
}

// TestCastDraftResultPreservesPriorExecutionError proves invalid metadata cannot replace or remap
// an already classified execution failure in the shared post-processing helper.
// TestCastDraftResultPreservesPriorExecutionError доказывает, что неверные metadata не заменяют
// и не переотображают уже классифицированную execution failure в общем post-processing helper.
func TestCastDraftResultPreservesPriorExecutionError(t *testing.T) {
	failure := status.Error(codes.Aborted, "already classified")
	draft, err := castDraftResult(value.DraftResult{}, failure)
	if draft != nil || err != failure {
		t.Fatal("metadata conversion replaced the prior execution error")
	}
}

// TestSaveDraftUnavailableStillClearsPlaintext checks the original deferred wipe even when no
// command service is configured; it neither consumes a grant nor creates a draft.
// TestSaveDraftUnavailableStillClearsPlaintext проверяет прежнее отложенное обнуление даже без
// настроенного command service; grant не потребляется и draft не создаётся.
func TestSaveDraftUnavailableStillClearsPlaintext(t *testing.T) {
	request := &secretbrokerv1.SaveSecretDraftRequest{OperationGrant: draftRPCFixtureGrant, Value: []byte(draftRPCFixturePlaintext)}
	response, err := (&Server{}).SaveSecretDraft(t.Context(), request)
	if response != nil || status.Code(err) != codes.Unavailable || !bytes.Equal(request.Value, make([]byte, len(request.Value))) {
		t.Fatal("unavailable draft service retained plaintext or changed response")
	}
}
