package grpc

import (
	"context"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestQueryResponseRejectsMissingPrincipalBeforeRead checks missing verified context prevents
// callback execution and preserves Unauthenticated instead of remapping it as a domain failure.
// The callback would return both a partial response and Forbidden if incorrectly invoked. This
// tests the local admission guard, not mTLS verification or a successful owner read.
// TestQueryResponseRejectsMissingPrincipalBeforeRead проверяет запрет callback без verified
// context и сохранение Unauthenticated вместо повторного domain mapping. Ошибочно вызванный
// callback вернул бы partial response и Forbidden. Это проверка локального admission guard,
// а не mTLS verification либо успешного owner read.
func TestQueryResponseRejectsMissingPrincipalBeforeRead(t *testing.T) {
	t.Parallel()
	calls := 0
	response, err := queryResponse(t.Context(), cp.PlatformQueryService_GetBootstrapState_FullMethodName,
		func(value.Principal) (*int, error) {
			calls++
			return new(int), errs.ErrForbidden
		})
	if calls != 0 || response != nil || status.Code(err) != codes.Unauthenticated {
		t.Fatal("query callback ran before verified principal or authentication error was remapped")
	}
}

// TestPlatformQueriesAuthenticateBeforeValidationAndService checks representative converted
// handlers reject missing context before dereferencing a nil service or validating malformed
// run/artifact/provider filters or reading runtime configuration/secret metadata. It preserves
// the authentication-first error priority; actual method pins
// and the other handlers are checked by the full extraction proof, not inferred from this test.
// TestPlatformQueriesAuthenticateBeforeValidationAndService проверяет отказ representative
// handlers без context до разыменования nil service, проверки неверных run/artifact/provider
// filters либо чтения runtime configuration и метаданных секретов.
// Сохраняет приоритет authentication error; exact method pins и прочие handlers проверяет
// полный extraction proof, они не выводятся из этого теста.
func TestPlatformQueriesAuthenticateBeforeValidationAndService(t *testing.T) {
	t.Parallel()
	var server *Server
	for name, call := range map[string]func(context.Context) (bool, error){
		"bootstrap": func(ctx context.Context) (bool, error) {
			response, err := server.GetBootstrapState(ctx, nil)
			return response != nil, err
		},
		"invalid run state": func(ctx context.Context) (bool, error) {
			response, err := server.ListRuns(ctx, &cp.ListRunsRequest{States: []cp.RunState{cp.RunState_RUN_STATE_UNSPECIFIED}})
			return response != nil, err
		},
		"excessive artifact sources": func(ctx context.Context) (bool, error) {
			response, err := server.ListArtifacts(ctx, &cp.ListArtifactsRequest{SourceKinds: make([]cp.ArtifactSource, 6)})
			return response != nil, err
		},
		"invalid financial summary filters": func(ctx context.Context) (bool, error) {
			response, err := server.ListProviderAccounts(ctx, &cp.ListProviderAccountsRequest{FinancialSummaryOnly: true, Query: "invalid with financial summary"})
			return response != nil, err
		},
		"template variable context": func(ctx context.Context) (bool, error) {
			response, err := server.ListTemplateVariables(ctx, nil)
			return response != nil, err
		},
		"runtime secret draft metadata": func(ctx context.Context) (bool, error) {
			response, err := server.GetRuntimeSecretDraft(ctx, nil)
			return response != nil, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			nonempty, err := call(t.Context())
			if nonempty || status.Code(err) != codes.Unauthenticated {
				t.Fatal("handler validated input or accessed service before authentication")
			}
		})
	}
}
