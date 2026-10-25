package grpc

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestPrincipalResponseResolverAndErrors checks exact context/method/principal forwarding,
// one resolver/callback execution, unchanged resolver errors and single domain error mapping.
// Partial results are discarded on failure and successful nil pointers remain nil. The fake
// resolver tests orchestration, not cryptographic verification or live owner authority.
// TestPrincipalResponseResolverAndErrors проверяет точную передачу context/method/principal,
// однократный resolver/callback, неизменённые ошибки resolver и однократное domain mapping.
// Partial result при ошибке отбрасывается, успешный nil сохраняется. Fake resolver проверяет
// orchestration, а не криптографию либо живые полномочия владельца.
func TestPrincipalResponseResolverAndErrors(t *testing.T) {
	t.Parallel()
	authError := status.Error(codes.Unauthenticated, "synthetic authentication failure")
	rawAuthError := errors.New("synthetic resolver failure")
	unexpected := errors.New("synthetic domain failure")
	response := new(int)
	*response = 42
	for _, test := range []struct {
		name                         string
		resolverError, callbackError error
		response                     *int
		code                         codes.Code
	}{
		{"resolver_status", authError, errs.ErrForbidden, response, codes.Unauthenticated},
		{"resolver_raw", rawAuthError, nil, response, codes.Unknown},
		{"success", nil, nil, response, codes.OK},
		{"nil_success", nil, nil, nil, codes.OK},
		{"partial_forbidden", nil, errs.ErrForbidden, response, codes.PermissionDenied},
		{"partial_cancelled", nil, context.Canceled, response, codes.Canceled},
		{"partial_deadline", nil, context.DeadlineExceeded, response, codes.DeadlineExceeded},
		{"partial_unavailable", nil, errs.ErrUnavailable, response, codes.Unavailable},
		{"partial_unexpected", nil, unexpected, response, codes.Internal},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			method := "/synthetic.Exact/Method"
			principal := value.Principal{ActorID: "actor_owned", AuthorityTenant: "tenant_owned", ProjectRef: "project_owned", Permission: "permission.exact", CorrelationRef: "correlation_owned", CallerWorkload: "workload_owned", CredentialRevision: 7, CredentialAMR: []string{"synthetic"}}
			resolutions, calls := 0, 0
			got, err := principalResponse(ctx, method,
				func(actual context.Context, fullMethod string) (value.Principal, error) {
					resolutions++
					if actual != ctx || fullMethod != method {
						t.Fatal("resolver binding changed")
					}
					return principal, test.resolverError
				},
				func(actual value.Principal) (*int, error) {
					calls++
					if !reflect.DeepEqual(actual, principal) {
						t.Fatal("verified principal changed")
					}
					return test.response, test.callbackError
				})
			if resolutions != 1 {
				t.Fatal("resolver count changed")
			}
			if test.resolverError != nil {
				if calls != 0 || got != nil || err != test.resolverError {
					t.Fatal("resolver rejection was bypassed or remapped")
				}
			} else if test.callbackError != nil {
				if calls != 1 || got != nil {
					t.Fatal("partial result escaped or callback repeated")
				}
			} else if calls != 1 || got != test.response || err != nil {
				t.Fatal("success pointer or callback count changed")
			}
			if status.Code(err) != test.code {
				t.Fatal("error mapping changed")
			}
		})
	}
}

// roleImageMissingPrincipalCase invokes one real typed RPC with a nil request and expects
// Unauthenticated before any nil receiver/service access or input handling. It does not mint a
// verified context or exercise the domain transition.
// roleImageMissingPrincipalCase вызывает настоящий typed RPC с nil request и требует
// Unauthenticated до обращения к nil receiver/service либо обработки input. Verified context
// не создаёт и доменный переход не выполняет.
func roleImageMissingPrincipalCase[Request, Response any](call func(context.Context, *Request) (*Response, error)) func(*testing.T) {
	return func(t *testing.T) {
		response, err := call(t.Context(), nil)
		if response != nil || status.Code(err) != codes.Unauthenticated {
			t.Fatal("role-image handler reached input/service before authentication")
		}
	}
}

// TestRoleImageResponsesAuthenticateBeforeInputs checks all twenty converted read/management/
// build/admission/promotion handlers retain the real roleImagePrincipal gate. Nil receiver
// exposes premature service access; exact method/input ordering is established separately by
// full source equivalence. No live claim or verified context is manufactured.
// TestRoleImageResponsesAuthenticateBeforeInputs проверяет сохранение настоящего
// roleImagePrincipal во всех двадцати converted read/management/build/admission/promotion RPC.
// Nil receiver обнаруживает ранний service access; точный порядок method/input отдельно
// подтверждает full source equivalence. Живой claim либо verified context не создаётся.
func TestRoleImageResponsesAuthenticateBeforeInputs(t *testing.T) {
	t.Parallel()
	var server *RoleImageServer
	cases := map[string]func(*testing.T){
		"ListRoleEnvironments":              roleImageMissingPrincipalCase(server.ListRoleEnvironments),
		"ListRoleImageRecipes":              roleImageMissingPrincipalCase(server.ListRoleImageRecipes),
		"GetRoleImageRecipe":                roleImageMissingPrincipalCase(server.GetRoleImageRecipe),
		"ManageRoleImageRecipe":             roleImageMissingPrincipalCase(server.ManageRoleImageRecipe),
		"ClaimImageBuild":                   roleImageMissingPrincipalCase(server.ClaimImageBuild),
		"RenewImageBuild":                   roleImageMissingPrincipalCase(server.RenewImageBuild),
		"ReportImageBuildProgress":          roleImageMissingPrincipalCase(server.ReportImageBuildProgress),
		"CompleteImageBuild":                roleImageMissingPrincipalCase(server.CompleteImageBuild),
		"FailImageBuild":                    roleImageMissingPrincipalCase(server.FailImageBuild),
		"ClaimImageAdmission":               roleImageMissingPrincipalCase(server.ClaimImageAdmission),
		"GetImageSupplyWorkAvailability":    roleImageMissingPrincipalCase(server.GetImageSupplyWorkAvailability),
		"RecordImageAdmission":              roleImageMissingPrincipalCase(server.RecordImageAdmission),
		"FailImageAdmission":                roleImageMissingPrincipalCase(server.FailImageAdmission),
		"ExpireImageAdmissionClaim":         roleImageMissingPrincipalCase(server.ExpireImageAdmissionClaim),
		"ClaimImagePromotion":               roleImageMissingPrincipalCase(server.ClaimImagePromotion),
		"AuthorizeImagePromotion":           roleImageMissingPrincipalCase(server.AuthorizeImagePromotion),
		"CompleteImagePromotion":            roleImageMissingPrincipalCase(server.CompleteImagePromotion),
		"ListOrganizationRoleImageRecipes":  roleImageMissingPrincipalCase(server.ListOrganizationRoleImageRecipes),
		"GetOrganizationRoleImageRecipe":    roleImageMissingPrincipalCase(server.GetOrganizationRoleImageRecipe),
		"ManageOrganizationRoleImageRecipe": roleImageMissingPrincipalCase(server.ManageOrganizationRoleImageRecipe),
	}
	for name, run := range cases {
		t.Run(name, run)
	}
}
