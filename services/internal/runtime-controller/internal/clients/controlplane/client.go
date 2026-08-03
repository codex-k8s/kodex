// Package controlplane адаптирует generated client к доменному порту.
package controlplane

import (
	"context"
	"errors"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/errs"
	runtimerepo "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type Mode string

const (
	ModeController        Mode = "controller"
	ModeRestoreVerifier   Mode = "restore-verifier"
	ModeCleanupAuthorizer Mode = "cleanup-authorizer"
)

type Config struct {
	Mode                                                                       Mode
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	ApplicationGrantFile                                                       string
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	DialTimeout                                                                time.Duration
}

type Client struct {
	shared *sharedclient.Client
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	var operations map[string]string
	switch config.Mode {
	case ModeController:
		operations = sharedclient.RuntimeControllerOperations()
	case ModeRestoreVerifier:
		operations = sharedclient.RuntimeRestoreVerifierOperations()
	case ModeCleanupAuthorizer:
		operations = sharedclient.RuntimeCleanupAuthorizerOperations()
	default:
		return nil, errors.New("control-plane client mode is invalid")
	}
	client, err := sharedclient.Dial(ctx, sharedclient.Config{
		Target:                config.Target,
		TLSServerName:         config.TLSServerName,
		CAFile:                config.CAFile,
		ClientCertificateFile: config.ClientCertificateFile,
		ClientPrivateKeyFile:  config.ClientPrivateKeyFile,
		ApplicationGrantFile:  config.ApplicationGrantFile,
		ExpectedIssuerUID:     config.ExpectedIssuerUID,
		ExpectedIssuerGID:     config.ExpectedIssuerGID,
		DialTimeout:           config.DialTimeout,
		Operations:            operations,
	})
	if err != nil {
		return nil, err
	}
	return &Client{shared: client}, nil
}

func (client *Client) Check(ctx context.Context) error { return client.shared.Check(ctx) }
func (client *Client) Close() error                    { return client.shared.Close() }

func (client *Client) Claim(ctx context.Context, key string) (entity.Execution, error) {
	response, err := client.shared.ControlPlane.ClaimRuntimeExecution(
		ctx, &controlplanev1.ClaimRuntimeExecutionRequest{IdempotencyKey: key},
	)
	if err != nil {
		return entity.Execution{}, mapError(err)
	}
	return castExecution(response.GetExecution())
}

func (client *Client) GetExecution(
	ctx context.Context, id string, expectedVersion uint64,
) (entity.Execution, error) {
	response, err := client.shared.ControlPlane.GetRuntimeExecution(
		ctx, &controlplanev1.GetRuntimeExecutionRequest{
			ExecutionId: id, ExpectedVersion: expectedVersion,
		},
	)
	if err != nil {
		return entity.Execution{}, mapError(err)
	}
	return castExecution(response.GetExecution())
}

func (client *Client) GetRevision(
	ctx context.Context, id string, expectedVersion uint64,
) (entity.Revision, error) {
	response, err := client.shared.ControlPlane.GetRuntimeRevision(
		ctx, &controlplanev1.GetRuntimeRevisionRequest{
			RuntimeRevisionId: id, ExpectedVersion: expectedVersion,
		},
	)
	if err != nil {
		return entity.Revision{}, mapError(err)
	}
	resource := response.GetRuntimeRevision()
	if resource == nil || resource.GetSpec() == nil {
		return entity.Revision{}, errs.ErrStateConflict
	}
	spec := resource.GetSpec().GetRuntimeRevision()
	if spec == nil {
		return entity.Revision{}, errs.ErrStateConflict
	}
	revision := entity.Revision{
		ID: resource.GetId(), Version: resource.GetVersion(),
		ManifestSHA256: spec.GetManifestSha256(), ImageDigest: spec.GetImageDigest(),
		SessionID: spec.GetSessionId(), RoleID: spec.GetRoleId(), ChatID: spec.GetChatId(),
		ProviderCredentialBindingID: spec.GetProviderCredentialBindingId(),
		PromptProfileID:             spec.GetPromptProfileId(), PromptRevision: spec.GetPromptRevision(),
		AuthorityPolicyRevision: spec.GetAuthorityPolicyRevision(),
		AuthorityPolicySHA256:   spec.GetAuthorityPolicySha256(),
		CredentialBindingIDs:    append([]string(nil), spec.GetCredentialBindingIds()...),
		IntegrationIDs:          append([]string(nil), spec.GetIntegrationIds()...),
	}
	for _, component := range spec.GetComponents() {
		revision.Components = append(revision.Components, entity.Component{
			Kind: component.GetKind().String(), ResourceID: component.GetResourceId(),
			Version: component.GetVersion(), ProjectionSHA256: component.GetProjectionSha256(),
		})
		if component.GetKind() == controlplanev1.ResourceKind_RESOURCE_KIND_CREDENTIAL_BINDING {
			credentialResponse, credentialErr := client.shared.ControlPlane.GetResource(
				ctx, &controlplanev1.GetResourceRequest{
					ResourceId:      component.GetResourceId(),
					ExpectedKind:    controlplanev1.ResourceKind_RESOURCE_KIND_CREDENTIAL_BINDING,
					ExpectedVersion: component.GetVersion(),
				},
			)
			if credentialErr != nil {
				return entity.Revision{}, mapError(credentialErr)
			}
			credential := credentialResponse.GetResource()
			if credential == nil || credential.GetSpec() == nil {
				return entity.Revision{}, errs.ErrStateConflict
			}
			credentialSpec := credential.GetSpec().GetCredentialBinding()
			if credentialSpec == nil ||
				credential.GetId() != component.GetResourceId() ||
				credential.GetVersion() != component.GetVersion() {
				return entity.Revision{}, errs.ErrStateConflict
			}
			revision.Credentials = append(revision.Credentials, entity.CredentialRef{
				ResourceID: credential.GetId(), Purpose: credentialSpec.GetPurpose(),
				Reference: credentialSpec.GetSecretRef(), Version: credential.GetVersion(),
			})
		}
	}
	return revision, nil
}

func (client *Client) GetResourceSnapshot(
	ctx context.Context,
	id, kind string,
	expectedVersion uint64,
) ([]byte, error) {
	value, ok := controlplanev1.ResourceKind_value["RESOURCE_KIND_"+kind]
	if !ok || value == int32(controlplanev1.ResourceKind_RESOURCE_KIND_UNSPECIFIED) || expectedVersion == 0 {
		return nil, errs.ErrInvalidInput
	}
	expectedKind := controlplanev1.ResourceKind(value)
	response, err := client.shared.ControlPlane.GetResource(ctx, &controlplanev1.GetResourceRequest{
		ResourceId: id, ExpectedKind: expectedKind, ExpectedVersion: expectedVersion,
	})
	if err != nil {
		return nil, mapError(err)
	}
	resource := response.GetResource()
	if resource == nil || resource.GetId() != id || resource.GetKind() != expectedKind ||
		resource.GetVersion() != expectedVersion {
		return nil, errs.ErrStateConflict
	}
	raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(resource)
	if err != nil {
		return nil, errs.ErrStateConflict
	}
	return raw, nil
}

func (client *Client) Admit(
	ctx context.Context, key string, execution entity.Execution,
) (runtimerepo.AdmitResult, error) {
	response, err := client.shared.ControlPlane.AdmitRuntimeExecution(
		ctx, &controlplanev1.AdmitRuntimeExecutionRequest{
			IdempotencyKey: key, ExecutionId: execution.ID,
			ExpectedVersion: execution.Version, ExpectedFence: execution.Fence,
			ExpectedGrantGeneration: execution.GrantGeneration,
		},
	)
	if err != nil {
		return runtimerepo.AdmitResult{}, mapError(err)
	}
	updated, err := castExecution(response.GetExecution())
	return runtimerepo.AdmitResult{Execution: updated, LeaseToken: response.GetLeaseToken()}, err
}

func (client *Client) Heartbeat(
	ctx context.Context, key string, execution entity.Execution, leaseToken string,
) (entity.Execution, error) {
	response, err := client.shared.ControlPlane.HeartbeatRuntimeExecution(
		ctx, &controlplanev1.HeartbeatRuntimeExecutionRequest{
			IdempotencyKey: key, ExecutionId: execution.ID,
			ExpectedVersion: execution.Version, ExpectedFence: execution.Fence,
			ExpectedGrantGeneration: execution.GrantGeneration, LeaseToken: leaseToken,
		},
	)
	if err != nil {
		return entity.Execution{}, mapError(err)
	}
	return castExecution(response.GetExecution())
}

func (client *Client) Complete(
	ctx context.Context, key string, execution entity.Execution,
	leaseToken, outcome, reference, digest string,
) (entity.Execution, error) {
	terminal := controlplanev1.RuntimeTerminalOutcome_RUNTIME_TERMINAL_OUTCOME_FAILED
	if outcome == "SUCCEEDED" {
		terminal = controlplanev1.RuntimeTerminalOutcome_RUNTIME_TERMINAL_OUTCOME_SUCCEEDED
	}
	response, err := client.shared.ControlPlane.CompleteRuntimeExecution(
		ctx, &controlplanev1.CompleteRuntimeExecutionRequest{
			IdempotencyKey: key, ExecutionId: execution.ID,
			ExpectedVersion: execution.Version, ExpectedFence: execution.Fence,
			ExpectedGrantGeneration: execution.GrantGeneration, LeaseToken: leaseToken,
			Outcome: terminal, TerminalReference: reference, TerminalSha256: digest,
		},
	)
	if err != nil {
		return entity.Execution{}, mapError(err)
	}
	return castExecution(response.GetExecution())
}

func (client *Client) Incident(
	ctx context.Context, key string, execution entity.Execution,
	kind enum.IncidentKind, incidentID, evidence string,
) (entity.Execution, error) {
	incidentKind := controlplanev1.RuntimeIncidentKind_RUNTIME_INCIDENT_KIND_RECONCILE_FAILED
	switch kind {
	case enum.IncidentHeartbeatMissed:
		incidentKind = controlplanev1.RuntimeIncidentKind_RUNTIME_INCIDENT_KIND_HEARTBEAT_MISSED
	case enum.IncidentWorkloadUnavailable:
		incidentKind = controlplanev1.RuntimeIncidentKind_RUNTIME_INCIDENT_KIND_WORKLOAD_UNAVAILABLE
	}
	response, err := client.shared.ControlPlane.RecordRuntimeIncident(
		ctx, &controlplanev1.RecordRuntimeIncidentRequest{
			IdempotencyKey: key, ExecutionId: execution.ID,
			ExpectedVersion: execution.Version, ExpectedFence: execution.Fence,
			Kind: incidentKind, IncidentId: incidentID, EvidenceSha256: evidence,
		},
	)
	if err != nil {
		return entity.Execution{}, mapError(err)
	}
	return castExecution(response.GetExecution())
}

func (client *Client) Expire(ctx context.Context, key string) (entity.Execution, error) {
	response, err := client.shared.ControlPlane.ExpireRuntimeExecution(
		ctx, &controlplanev1.ExpireRuntimeExecutionRequest{IdempotencyKey: key},
	)
	if err != nil {
		return entity.Execution{}, mapError(err)
	}
	return castExecution(response.GetExecution())
}

func (client *Client) RecordArchive(
	ctx context.Context, key string, execution entity.Execution, reference, digest string,
) (entity.Execution, error) {
	response, err := client.shared.ControlPlane.RecordRuntimeArchive(
		ctx, &controlplanev1.RecordRuntimeArchiveRequest{
			IdempotencyKey: key, ExecutionId: execution.ID,
			ExpectedVersion: execution.Version, ExpectedFence: execution.Fence,
			ArchiveReference: reference, ArchiveSha256: digest,
		},
	)
	if err != nil {
		return entity.Execution{}, mapError(err)
	}
	return castExecution(response.GetExecution())
}

func (client *Client) VerifyRestore(
	ctx context.Context, key string, execution entity.Execution,
	archiveDigest, reference, proofDigest string,
) (entity.Execution, error) {
	response, err := client.shared.ControlPlane.VerifyRuntimeRestore(
		ctx, &controlplanev1.VerifyRuntimeRestoreRequest{
			IdempotencyKey: key, ExecutionId: execution.ID,
			ExpectedVersion: execution.Version, ExpectedFence: execution.Fence,
			ArchiveSha256: archiveDigest, RestoreProofReference: reference,
			RestoreProofSha256: proofDigest,
		},
	)
	if err != nil {
		return entity.Execution{}, mapError(err)
	}
	return castExecution(response.GetExecution())
}

func (client *Client) AuthorizeCleanup(
	ctx context.Context, key string, execution entity.Execution, generation uint64,
) (entity.Execution, error) {
	response, err := client.shared.ControlPlane.AuthorizeRuntimeCleanup(
		ctx, &controlplanev1.AuthorizeRuntimeCleanupRequest{
			IdempotencyKey: key, ExecutionId: execution.ID,
			ExpectedVersion: execution.Version, ExpectedFence: execution.Fence,
			ArchiveSha256:             execution.ArchiveSHA256,
			RestoreProofSha256:        execution.RestoreProofSHA256,
			ExpectedCleanupGeneration: generation,
		},
	)
	if err != nil {
		return entity.Execution{}, mapError(err)
	}
	return castExecution(response.GetExecution())
}

func (client *Client) ConsumeCleanup(
	ctx context.Context, key string, execution entity.Execution,
) (entity.Execution, error) {
	response, err := client.shared.ControlPlane.ConsumeRuntimeCleanupAuthorization(
		ctx, &controlplanev1.ConsumeRuntimeCleanupAuthorizationRequest{
			IdempotencyKey: key, ExecutionId: execution.ID,
			ExpectedVersion: execution.Version, ExpectedFence: execution.Fence,
			CleanupAuthorizationId:         execution.CleanupAuthorizationID,
			CleanupAuthorizationGeneration: execution.CleanupAuthorizationGeneration,
			ArchiveSha256:                  execution.ArchiveSHA256,
			RestoreProofSha256:             execution.RestoreProofSHA256,
		},
	)
	if err != nil {
		return entity.Execution{}, mapError(err)
	}
	return castExecution(response.GetExecution())
}

func (client *Client) ExpireCleanup(
	ctx context.Context, key string, execution entity.Execution,
) (entity.Execution, error) {
	response, err := client.shared.ControlPlane.ExpireRuntimeCleanupAuthorization(
		ctx, &controlplanev1.ExpireRuntimeCleanupAuthorizationRequest{
			IdempotencyKey: key, ExecutionId: execution.ID,
			ExpectedVersion: execution.Version, ExpectedFence: execution.Fence,
			CleanupAuthorizationId:         execution.CleanupAuthorizationID,
			CleanupAuthorizationGeneration: execution.CleanupAuthorizationGeneration,
		},
	)
	if err != nil {
		return entity.Execution{}, mapError(err)
	}
	return castExecution(response.GetExecution())
}

func castExecution(source *controlplanev1.RuntimeExecution) (entity.Execution, error) {
	if source == nil {
		return entity.Execution{}, errs.ErrStateConflict
	}
	execution := entity.Execution{
		ID: source.GetExecutionId(), OrganizationID: source.GetOrganizationId(),
		ProjectID: source.GetProjectId(), ProcessID: source.GetProcessId(),
		SessionID: source.GetSessionId(), ThreadID: source.GetThreadId(),
		RoleID: source.GetRoleId(), TurnID: source.GetTurnId(), Attempt: source.GetAttempt(),
		RuntimeRevisionID:      source.GetRuntimeRevisionId(),
		RuntimeRevisionVersion: source.GetRuntimeRevisionVersion(),
		RuntimeRevisionSHA256:  source.GetRuntimeRevisionSha256(),
		ImmutableInputSHA256:   source.GetImmutableInputSha256(),
		ResourceClass:          enum.ResourceClass(trimEnum(source.GetResourceClass().String(), "RUNTIME_RESOURCE_CLASS_")),
		AccessProfile:          enum.AccessProfile(trimEnum(source.GetClusterAccessProfile().String(), "CLUSTER_ACCESS_PROFILE_")),
		WorkloadID:             source.GetWorkloadId(), WorkloadSPIFFEID: source.GetWorkloadSpiffeId(),
		GrantGeneration: source.GetGrantGeneration(), Version: source.GetVersion(), Fence: source.GetFence(),
		State:   enum.ExecutionState(trimEnum(source.GetState().String(), "RUNTIME_EXECUTION_STATE_")),
		LeaseID: source.GetLeaseId(), ArchiveReference: source.GetArchiveReference(),
		ArchiveSHA256: source.GetArchiveSha256(), RestoreProofReference: source.GetRestoreProofReference(),
		RestoreProofSHA256:             source.GetRestoreProofSha256(),
		CleanupAuthorizationID:         source.GetCleanupAuthorizationId(),
		CleanupAuthorizationGeneration: source.GetCleanupAuthorizationGeneration(),
		CleanupAuthorizationState:      trimEnum(source.GetCleanupAuthorizationState().String(), "RUNTIME_CLEANUP_AUTHORIZATION_STATE_"),
	}
	if source.GetLeaseExpiresAt() != nil {
		execution.LeaseExpiresAt = source.GetLeaseExpiresAt().AsTime()
	}
	if source.GetCleanupAuthorizationExpiresAt() != nil {
		execution.CleanupAuthorizationExpiresAt = source.GetCleanupAuthorizationExpiresAt().AsTime()
	}
	if err := execution.Validate(); err != nil {
		return entity.Execution{}, errs.ErrStateConflict
	}
	return execution, nil
}

func trimEnum(value, prefix string) string { return strings.TrimPrefix(value, prefix) }

func mapError(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return errs.ErrNoWork
	case codes.Aborted, codes.AlreadyExists, codes.FailedPrecondition:
		return errs.ErrStateConflict
	case codes.ResourceExhausted:
		return errs.ErrCapacityDeferred
	case codes.Unavailable, codes.DeadlineExceeded:
		return errs.ErrDependency
	default:
		return err
	}
}
