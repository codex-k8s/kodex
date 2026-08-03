package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	continuationclient "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/continuation"
	domainservice "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/service/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var integrationNamespace = uuid.MustParse("d00f3f7b-80ed-4cad-8b40-89ab4375548d")

type Client struct {
	client     *controlplaneclient.Client
	catalog    *Catalog
	sessionTTL time.Duration
}

func New(client *controlplaneclient.Client, catalog *Catalog, sessionTTL time.Duration) (*Client, error) {
	if client == nil || catalog == nil || sessionTTL < time.Minute || sessionTTL > 24*time.Hour {
		return nil, errors.New("control-plane integration client configuration is invalid")
	}
	return &Client{client: client, catalog: catalog, sessionTTL: sessionTTL}, nil
}

func (client *Client) Resolve(ctx context.Context, bearer string) (domainservice.Authority, error) {
	requestContext, err := controlplaneclient.WithApplicationGrant(ctx, bearer)
	if err != nil {
		return domainservice.Authority{}, err
	}
	response, err := client.client.ControlPlane.ResolveIntegrationSession(requestContext, &controlplanev1.ResolveIntegrationSessionRequest{})
	if err != nil {
		return domainservice.Authority{}, err
	}
	value := response.GetContext()
	if value == nil || uuid.Validate(value.GetOrganizationId()) != nil || uuid.Validate(value.GetProjectId()) != nil ||
		uuid.Validate(value.GetOwnerActorId()) != nil || uuid.Validate(value.GetProcessId()) != nil ||
		uuid.Validate(value.GetSessionId()) != nil || value.GetSessionVersion() == 0 ||
		uuid.Validate(value.GetThreadId()) != nil || uuid.Validate(value.GetTurnId()) != nil ||
		value.GetTurnVersion() == 0 || value.GetAttempt() == 0 ||
		!validDigest(value.GetInputSha256()) || uuid.Validate(value.GetRuntimeRevisionId()) != nil ||
		value.GetRuntimeRevisionVersion() == 0 || !validDigest(value.GetRuntimeRevisionSha256()) ||
		!validDigest(value.GetRuntimeManifestSha256()) ||
		uuid.Validate(value.GetRoleId()) != nil || value.GetRoleVersion() == 0 || value.GetGrantGeneration() == 0 {
		return domainservice.Authority{}, errors.New("control-plane integration session response is invalid")
	}
	now := time.Now().UTC()
	tokenHash := sha256.Sum256([]byte(bearer))
	grantExpiresAt, err := applicationGrantExpiry(bearer)
	if err != nil || !grantExpiresAt.After(now.Add(time.Minute)) {
		return domainservice.Authority{}, errors.New("application grant lifetime is insufficient")
	}
	authority := domainservice.Authority{
		TenantID: value.GetOrganizationId(), ProjectID: value.GetProjectId(), OwnerActorID: value.GetOwnerActorId(),
		ProcessID: value.GetProcessId(), SessionID: value.GetSessionId(), ThreadID: value.GetThreadId(),
		SessionVersion: value.GetSessionVersion(), TurnID: value.GetTurnId(), TurnVersion: value.GetTurnVersion(),
		Attempt:     value.GetAttempt(),
		InputDigest: value.GetInputSha256(), RuntimeRevisionID: value.GetRuntimeRevisionId(),
		RuntimeRevisionVersion: value.GetRuntimeRevisionVersion(), RuntimeRevisionDigest: value.GetRuntimeRevisionSha256(),
		RuntimeManifestDigest: value.GetRuntimeManifestSha256(),
		RoleID:                value.GetRoleId(),
		RoleVersion:           value.GetRoleVersion(), GrantGeneration: value.GetGrantGeneration(),
		TokenDigest: hex.EncodeToString(tokenHash[:]), ApplicationGrant: bearer,
		ApplicationGrantExpiresAt: grantExpiresAt,
	}
	if len(value.GetIntegrations()) > 64 || len(value.GetRoleCapabilities()) > 256 {
		return domainservice.Authority{}, errors.New("control-plane integration set is too large")
	}
	seenRoleCapabilities := make(map[string]struct{}, len(value.GetRoleCapabilities()))
	for _, capability := range value.GetRoleCapabilities() {
		if capability == "" || len(capability) > 128 || strings.ContainsAny(capability, "\r\n") {
			return domainservice.Authority{}, errors.New("control-plane role capability is invalid")
		}
		if _, duplicate := seenRoleCapabilities[capability]; duplicate {
			return domainservice.Authority{}, errors.New("control-plane role capability is duplicated")
		}
		seenRoleCapabilities[capability] = struct{}{}
	}
	seenIntegrations := make(map[string]struct{}, len(value.GetIntegrations()))
	for _, integration := range value.GetIntegrations() {
		definition, ok := client.catalog.Get(integration.GetDefinitionRef(), integration.GetDefinitionVersion())
		if !ok || uuid.Validate(integration.GetIntegrationId()) != nil || integration.GetIntegrationVersion() == 0 ||
			!validDigest(integration.GetProjectionSha256()) || integration.GetEndpointRef() == "" ||
			len(integration.GetEndpointRef()) > 256 || strings.ContainsAny(integration.GetEndpointRef(), "\r\n") ||
			len(integration.GetCapabilities()) == 0 || len(integration.GetCapabilities()) > 128 ||
			len(integration.GetCredentialBindings()) > 64 {
			return domainservice.Authority{}, errors.New("control-plane integration binding is invalid")
		}
		if _, duplicate := seenIntegrations[integration.GetIntegrationId()]; duplicate {
			return domainservice.Authority{}, errors.New("control-plane integration binding is duplicated")
		}
		seenIntegrations[integration.GetIntegrationId()] = struct{}{}
		seenCapabilities := make(map[string]struct{}, len(integration.GetCapabilities()))
		for _, capability := range integration.GetCapabilities() {
			if capability == "" || len(capability) > 64 {
				return domainservice.Authority{}, errors.New("control-plane integration capability is invalid")
			}
			if _, duplicate := seenCapabilities[capability]; duplicate {
				return domainservice.Authority{}, errors.New("control-plane integration capability is duplicated")
			}
			seenCapabilities[capability] = struct{}{}
		}
		connection := entity.Connection{
			TenantID: value.GetOrganizationId(), ProjectID: value.GetProjectId(),
			IntegrationID: integration.GetIntegrationId(), IntegrationVersion: integration.GetIntegrationVersion(),
			IntegrationDigest: integration.GetProjectionSha256(),
			DefinitionID:      definition.ID, DefinitionVersion: definition.Version,
			EndpointRef: integration.GetEndpointRef(), Revision: integration.GetIntegrationVersion(),
			Generation: value.GetGrantGeneration(), Status: enum.ConnectionPending,
		}
		seenCredentials := make(map[string]struct{}, len(integration.GetCredentialBindings()))
		seenPurposes := make(map[string]struct{}, len(integration.GetCredentialBindings()))
		for _, credential := range integration.GetCredentialBindings() {
			if uuid.Validate(credential.GetCredentialBindingId()) != nil || credential.GetCredentialBindingVersion() == 0 ||
				!validDigest(credential.GetProjectionSha256()) || credential.GetPurpose() == "" ||
				len(credential.GetPurpose()) > 64 || credential.GetSecretRef() == "" || len(credential.GetSecretRef()) > 256 ||
				credential.GetPrincipalRef() == "" || len(credential.GetPrincipalRef()) > 256 || credential.GetCredentialRevision() == 0 {
				return domainservice.Authority{}, errors.New("control-plane credential binding is invalid")
			}
			if _, duplicate := seenCredentials[credential.GetCredentialBindingId()]; duplicate {
				return domainservice.Authority{}, errors.New("control-plane credential binding is duplicated")
			}
			if _, duplicate := seenPurposes[credential.GetPurpose()]; duplicate {
				return domainservice.Authority{}, errors.New("control-plane credential purpose is duplicated")
			}
			seenCredentials[credential.GetCredentialBindingId()] = struct{}{}
			seenPurposes[credential.GetPurpose()] = struct{}{}
			binding := entity.CredentialBinding{
				ID:       credential.GetCredentialBindingId(),
				Version:  credential.GetCredentialBindingVersion(),
				Revision: credential.GetCredentialRevision(), ProjectionDigest: credential.GetProjectionSha256(),
				Purpose: credential.GetPurpose(), SecretRef: credential.GetSecretRef(), PrincipalRef: credential.GetPrincipalRef(),
			}
			if credential.GetExpiresAt() != nil {
				expiresAt := credential.GetExpiresAt().AsTime()
				if !expiresAt.After(now) {
					return domainservice.Authority{}, errors.New("control-plane credential binding is expired")
				}
				binding.ExpiresAt = &expiresAt
				if connection.ExpiresAt == nil || expiresAt.Before(*connection.ExpiresAt) {
					connection.ExpiresAt = &expiresAt
				}
			}
			connection.CredentialBindingRefs = append(connection.CredentialBindingRefs, binding)
		}
		connection.ID = connectionSnapshotID(authority, connection)
		grantID := uuid.NewSHA1(integrationNamespace, []byte(strings.Join([]string{
			value.GetProcessId(), value.GetSessionId(), strconv.FormatUint(value.GetSessionVersion(), 10),
			value.GetThreadId(), value.GetTurnId(), strconv.FormatUint(value.GetTurnVersion(), 10),
			strconv.FormatUint(uint64(value.GetAttempt()), 10), value.GetInputSha256(),
			value.GetRuntimeRevisionId(), strconv.FormatUint(value.GetRuntimeRevisionVersion(), 10),
			value.GetRuntimeRevisionSha256(), value.GetRuntimeManifestSha256(),
			value.GetRoleId(), strconv.FormatUint(value.GetRoleVersion(), 10),
			integration.GetIntegrationId(), strconv.FormatUint(integration.GetIntegrationVersion(), 10),
			integration.GetProjectionSha256(), strconv.FormatUint(value.GetGrantGeneration(), 10),
		}, "\x00"))).String()
		permissions := make([]string, 0, len(value.GetRoleCapabilities()))
		for _, tool := range definition.Tools {
			if slices.Contains(integration.GetCapabilities(), tool.Capability) && slices.Contains(value.GetRoleCapabilities(), tool.Permission) {
				permissions = append(permissions, tool.Permission)
			}
		}
		grantExpiresAt := minTime(now.Add(client.sessionTTL), grantExpiresAt.Add(-15*time.Second))
		if connection.ExpiresAt != nil {
			grantExpiresAt = minTime(grantExpiresAt, connection.ExpiresAt.Add(-15*time.Second))
		}
		if !grantExpiresAt.After(now) {
			return domainservice.Authority{}, errors.New("control-plane integration grant lifetime is insufficient")
		}
		grant := entity.Grant{
			ID: grantID, TenantID: value.GetOrganizationId(), ProjectID: value.GetProjectId(),
			ProcessID: value.GetProcessId(), SessionID: value.GetSessionId(), SessionVersion: value.GetSessionVersion(),
			ThreadID: value.GetThreadId(), TurnID: value.GetTurnId(), TurnVersion: value.GetTurnVersion(),
			Attempt:     value.GetAttempt(),
			InputDigest: value.GetInputSha256(), RuntimeRevisionID: value.GetRuntimeRevisionId(),
			RuntimeRevisionVersion: value.GetRuntimeRevisionVersion(),
			RuntimeRevisionDigest:  value.GetRuntimeRevisionSha256(),
			RuntimeManifestDigest:  value.GetRuntimeManifestSha256(),
			RoleID:                 value.GetRoleId(), RoleVersion: value.GetRoleVersion(),
			IntegrationID: integration.GetIntegrationId(), ConnectionID: connection.ID,
			Capabilities: slices.Clone(integration.GetCapabilities()), Permissions: permissions,
			Generation: value.GetGrantGeneration(), Status: enum.GrantActive,
			ExpiresAt: grantExpiresAt,
		}
		authority.Connections = append(authority.Connections, connection)
		authority.Grants = append(authority.Grants, grant)
	}
	return authority, nil
}

func connectionSnapshotID(authority domainservice.Authority, connection entity.Connection) string {
	bindings := slices.Clone(connection.CredentialBindingRefs)
	sort.Slice(bindings, func(left, right int) bool { return bindings[left].ID < bindings[right].ID })
	parts := []string{
		authority.TenantID, authority.ProjectID, authority.RuntimeRevisionID,
		strconv.FormatUint(authority.RuntimeRevisionVersion, 10), authority.RuntimeRevisionDigest,
		authority.RuntimeManifestDigest, strconv.FormatUint(authority.GrantGeneration, 10),
		connection.IntegrationID, strconv.FormatUint(connection.IntegrationVersion, 10),
		connection.IntegrationDigest, connection.DefinitionID,
		strconv.FormatUint(connection.DefinitionVersion, 10), connection.EndpointRef,
	}
	for _, binding := range bindings {
		expiresAt := ""
		if binding.ExpiresAt != nil {
			expiresAt = binding.ExpiresAt.UTC().Format(time.RFC3339Nano)
		}
		parts = append(parts, binding.ID, strconv.FormatUint(binding.Version, 10),
			strconv.FormatUint(binding.Revision, 10), binding.ProjectionDigest,
			binding.Purpose, binding.SecretRef, binding.PrincipalRef, expiresAt)
	}
	return uuid.NewSHA1(integrationNamespace, []byte(strings.Join(parts, "\x00"))).String()
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func (client *Client) Apply(ctx context.Context, command continuationclient.Command) (continuationclient.State, error) {
	requestContext, err := controlplaneclient.WithApplicationGrant(ctx, command.ApplicationGrant)
	if err != nil {
		return continuationclient.State{}, err
	}
	var value *controlplanev1.IntegrationContinuation
	switch command.Action {
	case enum.ContinuationSuspend:
		credentials := make([]*controlplanev1.PinnedIntegrationResource, 0, len(command.CredentialBindings))
		for _, binding := range command.CredentialBindings {
			credentials = append(credentials, &controlplanev1.PinnedIntegrationResource{
				ResourceId: binding.ID, Version: binding.Version, ProjectionSha256: binding.Digest,
			})
		}
		response, callErr := client.client.ControlPlane.SuspendForIntegrationApproval(requestContext,
			&controlplanev1.SuspendForIntegrationApprovalRequest{
				IdempotencyKey: command.IdempotencyKey, InvocationId: command.InvocationID,
				ApprovalId: command.ApprovalID, IntegrationId: command.IntegrationID,
				RequestSha256: command.RequestDigest, ApprovalExpiresAt: timestamppb.New(command.ApprovalExpiresAt),
				SelectedBinding: &controlplanev1.IntegrationApprovalBinding{
					Integration: &controlplanev1.PinnedIntegrationResource{
						ResourceId: command.IntegrationID,
						Version:    command.IntegrationVersion, ProjectionSha256: command.IntegrationDigest,
					},
					CredentialBindings: credentials,
				},
			})
		if callErr != nil {
			return continuationclient.State{}, callErr
		}
		value = response.GetContinuation()
	case enum.ContinuationApprove, enum.ContinuationReject, enum.ContinuationCancel:
		decision := &controlplanev1.IntegrationDecisionReference{
			IdempotencyKey: command.IdempotencyKey, ContinuationId: command.ContinuationID,
			ExpectedVersion: command.ExpectedVersion, ExpectedFence: command.ExpectedFence,
			InvocationId: command.InvocationID, ApprovalId: command.ApprovalID,
			RequestSha256: command.RequestDigest, DecisionReference: command.DecisionReference,
			DecisionSha256: command.DecisionDigest,
		}
		if command.Action == enum.ContinuationApprove {
			response, callErr := client.client.ControlPlane.ApproveIntegrationInvocation(requestContext,
				&controlplanev1.ApproveIntegrationInvocationRequest{Decision: decision})
			if callErr != nil {
				return continuationclient.State{}, callErr
			}
			value = response.GetContinuation()
		} else if command.Action == enum.ContinuationReject {
			response, callErr := client.client.ControlPlane.RejectIntegrationInvocation(requestContext,
				&controlplanev1.RejectIntegrationInvocationRequest{Decision: decision})
			if callErr != nil {
				return continuationclient.State{}, callErr
			}
			value = response.GetContinuation()
		} else {
			response, callErr := client.client.ControlPlane.CancelIntegrationInvocation(requestContext,
				&controlplanev1.CancelIntegrationInvocationRequest{Decision: decision})
			if callErr != nil {
				return continuationclient.State{}, callErr
			}
			value = response.GetContinuation()
		}
	case enum.ContinuationExpire:
		response, callErr := client.client.ControlPlane.ExpireIntegrationInvocation(requestContext,
			&controlplanev1.ExpireIntegrationInvocationRequest{IdempotencyKey: command.IdempotencyKey})
		if callErr != nil {
			return continuationclient.State{}, callErr
		}
		value = response.GetContinuation()
	case enum.ContinuationBegin:
		response, callErr := client.client.ControlPlane.BeginIntegrationExecution(requestContext,
			&controlplanev1.BeginIntegrationExecutionRequest{
				IdempotencyKey: command.IdempotencyKey,
				ContinuationId: command.ContinuationID, ExpectedVersion: command.ExpectedVersion,
				ExpectedFence: command.ExpectedFence, InvocationId: command.InvocationID,
				RequestSha256: command.RequestDigest,
			})
		if callErr != nil {
			return continuationclient.State{}, callErr
		}
		value = response.GetContinuation()
	case enum.ContinuationSucceed:
		response, callErr := client.client.ControlPlane.CompleteIntegrationExecution(requestContext,
			&controlplanev1.CompleteIntegrationExecutionRequest{
				IdempotencyKey: command.IdempotencyKey,
				ContinuationId: command.ContinuationID, ExpectedVersion: command.ExpectedVersion,
				ExpectedFence: command.ExpectedFence, InvocationId: command.InvocationID,
				RequestSha256: command.RequestDigest, ResultReference: command.ResultReference,
				ResultSha256: command.ResultDigest,
			})
		if callErr != nil {
			return continuationclient.State{}, callErr
		}
		value = response.GetContinuation()
	case enum.ContinuationFail:
		response, callErr := client.client.ControlPlane.FailIntegrationExecution(requestContext,
			&controlplanev1.FailIntegrationExecutionRequest{
				IdempotencyKey: command.IdempotencyKey,
				ContinuationId: command.ContinuationID, ExpectedVersion: command.ExpectedVersion,
				ExpectedFence: command.ExpectedFence, InvocationId: command.InvocationID,
				RequestSha256: command.RequestDigest, ErrorCode: command.ErrorCode,
				ErrorReference: command.ErrorReference, ErrorSha256: command.ErrorDigest,
			})
		if callErr != nil {
			return continuationclient.State{}, callErr
		}
		value = response.GetContinuation()
	default:
		return continuationclient.State{}, errors.New("control-plane continuation action is invalid")
	}
	if value == nil || uuid.Validate(value.GetContinuationId()) != nil || value.GetVersion() == 0 || value.GetFence() == 0 ||
		value.GetInvocationId() != command.InvocationID || value.GetRequestSha256() != command.RequestDigest {
		return continuationclient.State{}, errors.New("control-plane continuation response is invalid")
	}
	approvalState := strings.TrimPrefix(value.GetApprovalState().String(), "INTEGRATION_APPROVAL_STATE_")
	executionState := strings.TrimPrefix(value.GetExecutionState().String(), "INTEGRATION_EXECUTION_STATE_")
	continuationState := strings.TrimPrefix(value.GetContinuationState().String(), "INTEGRATION_CONTINUATION_STATE_")
	if approvalState == "UNSPECIFIED" || executionState == "UNSPECIFIED" || continuationState == "UNSPECIFIED" {
		return continuationclient.State{}, errors.New("control-plane continuation state is invalid")
	}
	var transitionExpiresAt time.Time
	if value.GetTransitionGrant() != "" {
		if value.GetTransitionGrantExpiresAt() == nil {
			return continuationclient.State{}, errors.New("control-plane transition grant expiry is missing")
		}
		transitionExpiresAt = value.GetTransitionGrantExpiresAt().AsTime()
		parsedExpiry, expiryErr := applicationGrantExpiry(value.GetTransitionGrant())
		if expiryErr != nil || !parsedExpiry.Equal(transitionExpiresAt) || !transitionExpiresAt.After(time.Now().UTC()) {
			return continuationclient.State{}, errors.New("control-plane transition grant is invalid")
		}
	} else if continuationState == "SUSPENDED" {
		return continuationclient.State{}, errors.New("control-plane transition grant is unavailable")
	}
	return continuationclient.State{
		ID: value.GetContinuationId(), Version: value.GetVersion(), Fence: value.GetFence(),
		ApprovalState: approvalState, ExecutionState: executionState,
		ContinuationState: continuationState, TransitionGrant: value.GetTransitionGrant(),
		TransitionGrantExpiresAt: transitionExpiresAt,
	}, nil
}

func (client *Client) Check(ctx context.Context) error {
	return client.client.Check(ctx)
}

func applicationGrantExpiry(compact string) (time.Time, error) {
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("application grant format is invalid")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(payload) == 0 || len(payload) > 16<<10 {
		return time.Time{}, errors.New("application grant payload is invalid")
	}
	var claims struct {
		ExpiresAt int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.ExpiresAt <= 0 {
		return time.Time{}, errors.New("application grant expiry is invalid")
	}
	return time.Unix(claims.ExpiresAt, 0).UTC(), nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, symbol := range value {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}
