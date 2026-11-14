package grpc

import (
	"context"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestPersonalIntegrationCredential queues only the authenticated user's versioned binding test;
// the owner resolves the health operation and stores its safe snapshot before returning CHECKING.
// TestPersonalIntegrationCredential ставит в очередь только versioned проверку собственной привязки;
// владелец разрешает health operation и сохраняет safe snapshot до ответа CHECKING.
func (server *Server) TestPersonalIntegrationCredential(ctx context.Context, request *cp.TestPersonalIntegrationCredentialRequest) (*cp.TestPersonalIntegrationCredentialResponse, error) {
	p, err := principal(ctx, cp.PersonalIntegrationCredentialService_TestPersonalIntegrationCredential_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.Execute(ctx, command.Command{Kind: command.TestPersonalIntegrationCredential, Principal: p, Mutation: mutation(request.GetMutation()),
		Payload: command.PersonalIntegrationCredentialInput{ConnectionRef: request.GetConnectionRef(), ExpectedConnectionVersion: request.GetExpectedConnectionVersion(), GitHubDiscoveryPage: int(request.GetGithubDiscoveryPage())}})
	if err != nil {
		return nil, transportError(err)
	}
	if result.PersonalIntegrationCredential == nil {
		return nil, status.Error(codes.Unavailable, personalCredentialResultUnavailable)
	}
	view, err := castPersonalIntegrationCredential(*result.PersonalIntegrationCredential, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &cp.TestPersonalIntegrationCredentialResponse{Credential: view}, nil
}

// ClaimPersonalIntegrationCredentialTests projects worker-only claims through the existing typed
// health envelope; it never returns this descriptor-bearing envelope through the browser API.
// ClaimPersonalIntegrationCredentialTests преобразует worker-only claims существующим typed
// health envelope; этот envelope с descriptors не возвращается browser API.
func (server *Server) ClaimPersonalIntegrationCredentialTests(ctx context.Context, request *cp.ClaimPersonalIntegrationCredentialTestsRequest) (*cp.ClaimPersonalIntegrationCredentialTestsResponse, error) {
	p, err := principal(ctx, cp.PersonalIntegrationCredentialService_ClaimPersonalIntegrationCredentialTests_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, err := server.service.ClaimPersonalIntegrationCredentialTests(ctx, p, request.GetWorkloadInstance(), request.GetLimit())
	if err != nil {
		return nil, transportError(err)
	}
	result := &cp.ClaimPersonalIntegrationCredentialTestsResponse{}
	for _, item := range items {
		result.Claims = append(result.Claims, CastIntegrationConnectionTestClaim(item))
	}
	return result, nil
}

// BeginPersonalIntegrationCredentialProbe consumes one exact lease immediately before the external
// READ. Repeating an admitted request is rejected instead of authorizing another provider call.
// BeginPersonalIntegrationCredentialProbe расходует один exact lease непосредственно перед внешним
// READ. Повтор допущенного запроса отвергается, не разрешая второй provider call.
func (server *Server) BeginPersonalIntegrationCredentialProbe(ctx context.Context, request *cp.BeginPersonalIntegrationCredentialProbeRequest) (*cp.BeginPersonalIntegrationCredentialProbeResponse, error) {
	p, err := principal(ctx, cp.PersonalIntegrationCredentialService_BeginPersonalIntegrationCredentialProbe_FullMethodName)
	if err != nil {
		return nil, err
	}
	err = server.service.BeginPersonalIntegrationCredentialProbe(ctx, p, command.PersonalIntegrationCredentialTestCompletion{TestRef: request.GetTestRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration()})
	if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrNotFound) {
		return &cp.BeginPersonalIntegrationCredentialProbeResponse{Admitted: false}, nil
	}
	if err != nil {
		return nil, transportError(err)
	}
	return &cp.BeginPersonalIntegrationCredentialProbeResponse{Admitted: true}, nil
}

// CompletePersonalIntegrationCredentialTest maps bounded probe observations from the registered
// worker; the owner checks keys against its immutable snapshot, with no subject/credential selector.
// CompletePersonalIntegrationCredentialTest преобразует bounded observations зарегистрированного
// worker; owner сверяет keys с immutable snapshot, без selector subject/credential.
func (server *Server) CompletePersonalIntegrationCredentialTest(ctx context.Context, request *cp.CompletePersonalIntegrationCredentialTestRequest) (*cp.CompletePersonalIntegrationCredentialTestResponse, error) {
	p, err := principal(ctx, cp.PersonalIntegrationCredentialService_CompletePersonalIntegrationCredentialTest_FullMethodName)
	if err != nil {
		return nil, err
	}
	var outcome entity.PersonalIntegrationCredentialTestOutcome
	switch request.GetResult() {
	case cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_AUTHENTICATED:
		outcome = entity.PersonalIntegrationCredentialAuthenticated
	case cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_AUTH_REJECTED:
		outcome = entity.PersonalIntegrationCredentialAuthenticationRejected
	case cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_UNAVAILABLE:
		outcome = entity.PersonalIntegrationCredentialVerificationUnavailable
	default:
		return nil, status.Error(codes.InvalidArgument, "personal integration test result is invalid")
	}
	if len(request.GetConfirmedCapabilityKeys()) > 128 || len(request.GetDeniedCapabilityKeys()) > 128 || len(request.GetObservedPermissionProfiles()) > 1 {
		return nil, status.Error(codes.InvalidArgument, "personal integration probe observation is invalid")
	}
	profiles := make([]string, 0, len(request.GetObservedPermissionProfiles()))
	for _, profile := range request.GetObservedPermissionProfiles() {
		if profile != cp.PersonalIntegrationCredentialObservationProfile_PERSONAL_INTEGRATION_CREDENTIAL_OBSERVATION_PROFILE_GITLAB_PROJECT_WRITE_SCOPE_V1 {
			return nil, status.Error(codes.InvalidArgument, "personal integration metadata profile is invalid")
		}
		profiles = append(profiles, integrationpackage.CredentialObservationGitLabProjectWriteScopeV1)
	}
	var expiry *time.Time
	if request.GetExpiresAt() != nil {
		if request.GetExpiresAt().CheckValid() != nil {
			return nil, status.Error(codes.InvalidArgument, "personal integration expiry is invalid")
		}
		value := request.GetExpiresAt().AsTime()
		expiry = &value
	}
	discovery, valid := parseGitHubDiscovery(request.GetGithubDiscovery())
	if !valid {
		return nil, status.Error(codes.InvalidArgument, "GitHub discovery result is invalid")
	}
	result, err := server.service.Execute(ctx, command.Command{Kind: command.CompletePersonalIntegrationCredentialTest, Principal: p, Mutation: mutation(request.GetMutation()), Payload: command.PersonalIntegrationCredentialTestCompletion{
		TestRef: request.GetTestRef(), LeaseRef: request.GetLeaseRef(), Fence: request.GetFence(), Generation: request.GetGeneration(), Outcome: outcome, ConfirmedCapabilityKeys: append([]string(nil), request.GetConfirmedCapabilityKeys()...), DeniedCapabilityKeys: append([]string(nil), request.GetDeniedCapabilityKeys()...), ObservedPermissionProfiles: profiles, ExpiresAt: expiry, GitHubDiscovery: discovery}})
	if err != nil {
		return nil, transportError(err)
	}
	receipt := result.PersonalIntegrationCredentialTest
	if receipt == nil || receipt.TestRef != request.GetTestRef() || (receipt.State != "SUCCEEDED" && receipt.State != "FAILED" && receipt.State != "CANCELLED") {
		return nil, status.Error(codes.Unavailable, personalCredentialResultUnavailable)
	}
	return &cp.CompletePersonalIntegrationCredentialTestResponse{TestRef: receipt.TestRef, State: receipt.State, Applied: receipt.Applied}, nil
}
