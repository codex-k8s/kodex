package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	api "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// diagnosticProofProvider supplies one synthetic proof or a typed provider failure to the real
// interceptor, without a network, credentials, or downstream business effects.
// diagnosticProofProvider передаёт настоящий interceptor один synthetic proof либо typed failure
// поставщика, без сети, credentials и внешнего бизнес-эффекта.
type diagnosticProofProvider struct {
	failure error
	calls   int
}

// AuthorityProof records one bounded attempt and returns only the configured synthetic result.
// AuthorityProof считает одну ограниченную попытку и возвращает заданный synthetic результат.
func (provider *diagnosticProofProvider) AuthorityProof(context.Context, string, string) (string, string, error) {
	provider.calls++
	return "synthetic-private-proof", "synthetic-private-correlation", provider.failure
}

// diagnosticOperationResolver admits only the single test method; it cannot mint real authority.
// diagnosticOperationResolver разрешает лишь один тестовый метод и не создаёт реальные полномочия.
type diagnosticOperationResolver struct{}

// OperationID maps exactly the synthetic method used by this isolated test.
// OperationID сопоставляет только synthetic метод этой изолированной проверки.
func (diagnosticOperationResolver) OperationID(method string) (string, bool) {
	return "synthetic.renew", method == "/synthetic.RoleImages/Renew"
}

// diagnosticRejectedIssuer implements only the expected failing issue call; no other RPC is used.
// diagnosticRejectedIssuer реализует только ожидаемый отказ issue call; другие RPC не вызываются.
type diagnosticRejectedIssuer struct {
	api.AuthorizationIssuerServiceClient
	failure error
	calls   int
}

// IssueAuthorizationContext records one attempt and rejects before any context is returned.
// IssueAuthorizationContext считает одну попытку и отклоняет её до выдачи контекста.
func (issuer *diagnosticRejectedIssuer) IssueAuthorizationContext(context.Context, *api.IssueAuthorizationContextRequest, ...grpc.CallOption) (*api.IssueAuthorizationContextResponse, error) {
	issuer.calls++
	return nil, issuer.failure
}

// TestBuildCyclePreservesTypedAuthorityDiagnostic drives the actual SDK boundary: permanent
// dependency failures become Unauthenticated externally but retain normalized local classification.
// Wrapping/joining cannot leak the original message, correlation, proof or error chain to either sink.
// TestBuildCyclePreservesTypedAuthorityDiagnostic вызывает настоящую SDK boundary: постоянный отказ
// зависимости становится внешним Unauthenticated, сохраняя нормализованную локальную классификацию.
// Wrapping/joining не раскрывают исходный текст, correlation, proof или error chain ни одному sink.
func TestBuildCyclePreservesTypedAuthorityDiagnostic(t *testing.T) {
	for _, source := range []string{"resolver", "issuer"} {
		for _, wrapper := range []string{"direct", "wrapped", "joined"} {
			t.Run(source+"/"+wrapper, func(t *testing.T) {
				dependency := status.Error(codes.PermissionDenied, "synthetic-sensitive-detail")
				provider := &diagnosticProofProvider{}
				issuer := &diagnosticRejectedIssuer{failure: dependency}
				wantStage := "context_issue"
				if source == "resolver" {
					provider.failure = authorityclient.NewProofFailure(authorityclient.StageProofResolve, dependency)
					wantStage = "proof_resolve"
				}
				invoked := false
				original := authorityclient.IssuerUnaryClientInterceptor(issuer, diagnosticOperationResolver{}, provider)(
					t.Context(), "/synthetic.RoleImages/Renew", &emptypb.Empty{}, &emptypb.Empty{}, nil,
					func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
						invoked = true
						return nil
					})
				var local *authorityclient.LocalAuthorityError
				if !errors.As(original, &local) || status.Code(original) != codes.Unauthenticated || local.Diagnostic().Code != codes.PermissionDenied || invoked || provider.calls != 1 {
					t.Fatal("real SDK boundary or no-effect contract mismatch")
				}
				if source == "issuer" && issuer.calls != 1 || source == "resolver" && issuer.calls != 0 {
					t.Fatal("unexpected issuer effect")
				}
				failure := original
				if wrapper == "wrapped" {
					failure = fmt.Errorf("synthetic-sensitive-detail: %w", original)
				}
				if wrapper == "joined" {
					failure = errors.Join(errors.New("synthetic-sensitive-detail"), original)
				}
				var output bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&output, nil))
				calls := 0
				reportBuildCycleDegraded(t.Context(), logger, func(_ context.Context, clean error) {
					calls++
					if errors.Is(clean, original) || errors.Unwrap(clean) != nil || errors.As(clean, &local) {
						t.Error("telemetry retained original chain")
					}
					for _, forbidden := range []string{"synthetic-sensitive-detail", "synthetic-private-proof", "synthetic-private-correlation"} {
						if strings.Contains(clean.Error(), forbidden) {
							t.Error("telemetry leaked synthetic private detail")
						}
					}
					if !strings.Contains(clean.Error(), "authority_stage="+wantStage) || !strings.Contains(clean.Error(), "dependency_code=PermissionDenied") {
						t.Error("telemetry lost normalized nested classification")
					}
				}, failure)
				var record map[string]any
				if json.Unmarshal(output.Bytes(), &record) != nil {
					t.Fatal("log is not JSON")
				}
				if calls != 1 || len(record) != 9 || record["rpc_code"] != "Unauthenticated" || record["authority_stage"] != wantStage || record["dependency_code"] != "PermissionDenied" {
					t.Fatal("typed closed projection mismatch")
				}
				for _, forbidden := range []string{"synthetic-sensitive-detail", "synthetic-private-proof", "synthetic-private-correlation"} {
					if strings.Contains(output.String(), forbidden) {
						t.Fatal("log leaked synthetic private detail")
					}
				}
			})
		}
	}
}

// TestBuildCycleRejectsForgedDiagnosticText keeps untyped diagnostic-looking messages outside
// structured fields and telemetry; only the concrete SDK error supplies additional enums.
// TestBuildCycleRejectsForgedDiagnosticText не допускает текст с поддельной diagnostic в поля
// журнала и telemetry; дополнительные enums предоставляет лишь конкретная SDK error.
func TestBuildCycleRejectsForgedDiagnosticText(t *testing.T) {
	var output bytes.Buffer
	original := errors.New("authority_stage=proof_resolve dependency_code=PermissionDenied synthetic-sensitive-detail")
	reportBuildCycleDegraded(t.Context(), slog.New(slog.NewJSONHandler(&output, nil)), func(_ context.Context, clean error) {
		if clean.Error() != buildCycleDegradedMessage+": Unknown" {
			t.Error("forged diagnostic entered telemetry")
		}
	}, original)
	var record map[string]any
	if json.Unmarshal(output.Bytes(), &record) != nil || len(record) != 5 || record["rpc_code"] != "Unknown" {
		t.Fatal("untyped error forged structured diagnostic")
	}
}
