package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"google.golang.org/protobuf/types/known/timestamppb"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
)

// personalCredentialProbeAdapter exposes only bounded health/registered READ verification;
// it cannot invoke arbitrary capabilities, select endpoints, write or execute a shell.
// personalCredentialProbeAdapter открывает только bounded health/registered READ verification;
// произвольные capabilities, endpoints, запись либо shell недоступны.
type personalCredentialProbeAdapter interface {
	// VerifyPersonalCredential returns safe metadata for its owner-pinned READ batch, never payload.
	// VerifyPersonalCredential возвращает safe metadata owner-pinned READ batch без payload.
	VerifyPersonalCredential(context.Context, integration.Request) (integration.CredentialProbeResult, error)
}

// processPersonalCredentialTests takes one job at a time so its finite lease is not spent in a
// local batch. Owner admission is consumed before its pinned READ batch, never retried after lost
// ACK; metrics count an attempted verification even when subsequent owner completion fails.
// processPersonalCredentialTests берёт по одному заданию, не расходуя finite lease в локальном batch.
// Owner admission расходуется перед pinned READ batch и не повторяется после lost ACK; metrics
// учитывают начатую verification даже при последующей ошибке owner completion.
func processPersonalCredentialTests(ctx context.Context, client cp.PersonalIntegrationCredentialServiceClient, adapter personalCredentialProbeAdapter, config Config, record func(bool)) (int, error) {
	if client == nil || adapter == nil || record == nil {
		return 0, errors.New("personal credential verifier is unavailable")
	}
	claims, err := client.ClaimPersonalIntegrationCredentialTests(ctx, &cp.ClaimPersonalIntegrationCredentialTestsRequest{WorkloadInstance: config.InstanceID, Limit: 1})
	if err != nil {
		return 0, err
	}
	if claims == nil || len(claims.Claims) > 1 {
		return 0, errors.New("personal credential claim response is invalid")
	}
	if len(claims.Claims) == 0 {
		return 0, nil
	}
	claim := claims.Claims[0]
	lease := claim.GetLease()
	if lease == nil || lease.GetExpiresAt() == nil || lease.GetExpiresAt().CheckValid() != nil || lease.GetGeneration() != 1 || lease.GetRef() == "" || lease.GetFence() == "" || claim.GetTestRef() == "" || claim.GetCredentialRevision() == nil ||
		lease.GetExpiresAt().AsTime().Before(time.Now().Add(config.OperationTimeout+config.RequestTimeout)) {
		return 0, errors.New("personal credential claim lease is invalid or insufficient")
	}
	admission, err := client.BeginPersonalIntegrationCredentialProbe(ctx, &cp.BeginPersonalIntegrationCredentialProbeRequest{
		TestRef: claim.GetTestRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration()})
	if err != nil {
		return 0, err
	}
	if admission == nil {
		return 0, errors.New("personal credential admission response is invalid")
	}
	if !admission.Admitted {
		return 1, nil
	}
	operation, cancel := context.WithTimeout(ctx, config.OperationTimeout)
	observation, operationErr := adapter.VerifyPersonalCredential(operation, integration.RequestFromTest(claim))
	cancel()
	record(operationErr == nil)
	outcome := cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_UNAVAILABLE
	success, code := integration.Outcome(operationErr)
	if success {
		outcome = cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_AUTHENTICATED
	} else if code == "INTEGRATION_AUTH_REJECTED" {
		outcome = cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_AUTH_REJECTED
	}
	if !success {
		observation = integration.CredentialProbeResult{}
	}
	profiles := make([]cp.PersonalIntegrationCredentialObservationProfile, 0, len(observation.ObservedPermissionProfiles))
	for _, profile := range observation.ObservedPermissionProfiles {
		if profile == integrationpackage.CredentialObservationSentryScopesV1 {
			profiles = append(profiles, cp.PersonalIntegrationCredentialObservationProfile_PERSONAL_INTEGRATION_CREDENTIAL_OBSERVATION_PROFILE_SENTRY_TOKEN_SCOPES_V1)
			continue
		}
		if profile != integrationpackage.CredentialObservationGitLabProjectWriteScopeV1 {
			return 1, errors.New("personal credential observation profile is invalid")
		}
		profiles = append(profiles, cp.PersonalIntegrationCredentialObservationProfile_PERSONAL_INTEGRATION_CREDENTIAL_OBSERVATION_PROFILE_GITLAB_PROJECT_WRITE_SCOPE_V1)
	}
	var expiry *timestamppb.Timestamp
	if observation.ExpiresAt != nil {
		expiry = timestamppb.New(*observation.ExpiresAt)
		if expiry.CheckValid() != nil {
			return 1, errors.New("personal credential observation expiry is invalid")
		}
	}
	receipt, err := client.CompletePersonalIntegrationCredentialTest(ctx, &cp.CompletePersonalIntegrationCredentialTestRequest{
		Mutation: &cp.MutationContext{IdempotencyKey: stableKey(claim.GetTestRef(), fmt.Sprintf("personal-complete:%d", lease.GetGeneration()))},
		TestRef:  claim.GetTestRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(), Result: outcome, ConfirmedCapabilityKeys: observation.ConfirmedCapabilityKeys, DeniedCapabilityKeys: observation.DeniedCapabilityKeys, ObservedPermissionProfiles: profiles, ExpiresAt: expiry, GithubDiscovery: personalGitHubDiscoveryProto(observation.GitHubDiscovery)})
	if err != nil {
		return 1, err
	}
	if receipt == nil || receipt.TestRef != claim.GetTestRef() || (receipt.State != "SUCCEEDED" && receipt.State != "FAILED" && receipt.State != "CANCELLED") ||
		(receipt.State == "CANCELLED" && receipt.Applied) || (receipt.State != "CANCELLED" && !receipt.Applied) ||
		(receipt.State == "SUCCEEDED" && !success) || (receipt.State == "FAILED" && success) {
		return 1, errors.New("personal credential completion receipt is invalid")
	}
	return 1, nil
}

// personalGitHubDiscoveryProto copies a validated safe page into the worker completion; no raw provider response or credential is serialized.
// personalGitHubDiscoveryProto копирует проверенную безопасную страницу в worker completion без сериализации provider response либо credential.
func personalGitHubDiscoveryProto(page *integrationpackage.GitHubDiscoveryPage) *cp.GitHubRepositoryDiscoveryPage {
	if page == nil || !page.Valid() {
		return nil
	}
	result := &cp.GitHubRepositoryDiscoveryPage{IdentityId: page.IdentityID, Login: page.Login, Page: int32(page.Page), NextPage: int32(page.NextPage), Items: []*cp.GitHubDiscoveredRepository{}}
	for _, repo := range page.Items {
		result.Items = append(result.Items, &cp.GitHubDiscoveredRepository{Id: repo.ID, FullName: repo.FullName, OwnerKind: repo.OwnerKind, Private: repo.Private, Archived: repo.Archived, CanRead: repo.CanRead, CanPush: repo.CanPush})
	}
	return result
}
