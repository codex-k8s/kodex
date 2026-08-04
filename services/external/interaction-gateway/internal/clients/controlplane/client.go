// Package controlplane реализует generated gRPC adapter interaction-gateway.
package controlplane

import (
	"context"
	"errors"
	"slices"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	domaincontrol "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/controlplane"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Client struct {
	client   *controlplaneclient.Client
	deadline time.Duration
}

func New(client *controlplaneclient.Client, deadline time.Duration) (*Client, error) {
	if client == nil || deadline < 500*time.Millisecond || deadline > 10*time.Second {
		return nil, errors.New("control-plane interaction client configuration is invalid")
	}
	return &Client{client: client, deadline: deadline}, nil
}

func (client *Client) Check(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	return client.client.Check(bounded)
}

func (client *Client) CheckInteraction(ctx context.Context, grant, projectID string) error {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	protected, err := controlplaneclient.WithApplicationGrant(bounded, grant)
	if err != nil {
		return err
	}
	response, err := client.client.ControlPlane.GetResource(protected, &controlplanev1.GetResourceRequest{
		ResourceId: projectID, ExpectedKind: controlplanev1.ResourceKind_RESOURCE_KIND_PROJECT,
	})
	if err != nil || response.GetResource() == nil || response.GetResource().GetId() != projectID {
		return errors.New("control-plane Mattermost event working path is not ready")
	}
	return nil
}

func (client *Client) RegisterArtifact(ctx context.Context, grant string, input domaincontrol.ArtifactInput) (domaincontrol.Artifact, error) {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	protected, err := controlplaneclient.WithApplicationGrant(bounded, grant)
	if err != nil {
		return domaincontrol.Artifact{}, err
	}
	response, err := client.client.ControlPlane.RegisterArtifact(protected, &controlplanev1.RegisterArtifactRequest{
		IdempotencyKey: input.IdempotencyKey, Name: input.Name, ParentId: input.ParentID,
		Spec: &controlplanev1.ArtifactSpec{
			Kind: input.Kind, Direction: input.Direction, StorageRef: input.StorageRef,
			SizeBytes: input.SizeBytes, MediaType: input.MediaType, Sha256: input.SHA256,
			RetentionPolicyRef: input.RetentionRef,
		},
	})
	if err != nil || response.GetArtifact() == nil || response.GetArtifact().GetId() == "" {
		return domaincontrol.Artifact{}, errors.New("register control-plane artifact")
	}
	resource := response.GetArtifact()
	return projectArtifact(resource), nil
}

func (client *Client) GetArtifact(ctx context.Context, grant, artifactID string, expectedVersion uint64) (domaincontrol.Artifact, error) {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	protected, err := controlplaneclient.WithApplicationGrant(bounded, grant)
	if err != nil {
		return domaincontrol.Artifact{}, err
	}
	response, err := client.client.ControlPlane.GetResource(protected, &controlplanev1.GetResourceRequest{
		ResourceId: artifactID, ExpectedKind: controlplanev1.ResourceKind_RESOURCE_KIND_ARTIFACT,
		ExpectedVersion: expectedVersion,
	})
	if err != nil || response.GetResource() == nil || response.GetResource().GetSpec().GetArtifact() == nil {
		return domaincontrol.Artifact{}, errors.New("read control-plane artifact")
	}
	resource := response.GetResource()
	return projectArtifact(resource), nil
}

func (client *Client) GetTurn(ctx context.Context, grant, turnID string) (domaincontrol.Turn, error) {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	protected, err := controlplaneclient.WithApplicationGrant(bounded, grant)
	if err != nil {
		return domaincontrol.Turn{}, err
	}
	response, err := client.client.ControlPlane.GetResource(protected, &controlplanev1.GetResourceRequest{
		ResourceId: turnID, ExpectedKind: controlplanev1.ResourceKind_RESOURCE_KIND_TURN,
	})
	resource := response.GetResource()
	if err != nil || resource == nil || resource.GetSpec().GetTurn() == nil {
		return domaincontrol.Turn{}, errors.New("read control-plane turn")
	}
	spec := resource.GetSpec().GetTurn()
	return domaincontrol.Turn{ID: resource.GetId(), Version: resource.GetVersion(),
		State:     stringWithoutPrefix(resource.GetState().String(), "RESOURCE_STATE_"),
		SessionID: spec.GetSessionId(), Attempt: spec.GetAttempt(), Outcome: spec.GetOutcome(),
		ResultArtifactID: spec.GetResultArtifactId(), ResultArtifactVersion: spec.GetResultArtifactVersion(),
		ResultArtifactSHA256: spec.GetResultArtifactSha256(), ImmutableInputSHA256: spec.GetEffectiveInputSha256()}, nil
}

func (client *Client) ManageConversationLifecycle(ctx context.Context, grant, idempotencyKey, kind, resourceID, action string) error {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	protected, err := controlplaneclient.WithApplicationGrant(bounded, grant)
	if err != nil {
		return err
	}
	kindValue := controlplanev1.ConversationLifecycleKind_CONVERSATION_LIFECYCLE_KIND_CHANNEL
	if kind == "THREAD" {
		kindValue = controlplanev1.ConversationLifecycleKind_CONVERSATION_LIFECYCLE_KIND_THREAD
	} else if kind != "CHANNEL" {
		return errors.New("conversation lifecycle kind is invalid")
	}
	actionValue := controlplanev1.ConversationLifecycleAction_CONVERSATION_LIFECYCLE_ACTION_DELETE
	switch action {
	case "RESTORE":
		actionValue = controlplanev1.ConversationLifecycleAction_CONVERSATION_LIFECYCLE_ACTION_RESTORE
	case "FINALIZE":
		actionValue = controlplanev1.ConversationLifecycleAction_CONVERSATION_LIFECYCLE_ACTION_FINALIZE
	case "DELETE":
	default:
		return errors.New("conversation lifecycle action is invalid")
	}
	response, err := client.client.ControlPlane.ManageConversationLifecycle(protected,
		&controlplanev1.ManageConversationLifecycleRequest{IdempotencyKey: idempotencyKey,
			Kind: kindValue, Action: actionValue, ResourceId: resourceID})
	if err != nil || response.GetResource() == nil || response.GetResource().GetId() != resourceID {
		return errors.New("manage control-plane conversation lifecycle")
	}
	return nil
}

func (client *Client) CreateSession(ctx context.Context, grant, idempotencyKey, name, roleID, conversationID string) (domaincontrol.Session, error) {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	protected, err := controlplaneclient.WithApplicationGrant(bounded, grant)
	if err != nil {
		return domaincontrol.Session{}, err
	}
	response, err := client.client.ControlPlane.ManageSession(protected, &controlplanev1.ManageSessionRequest{
		IdempotencyKey: idempotencyKey, Action: controlplanev1.SessionAction_SESSION_ACTION_CREATE,
		Name: name, RoleId: roleID, ConversationId: conversationID,
	})
	if err != nil || response.GetSession() == nil || response.GetSession().GetId() == "" {
		return domaincontrol.Session{}, errors.New("create control-plane session")
	}
	return domaincontrol.Session{ID: response.GetSession().GetId(), Version: response.GetSession().GetVersion()}, nil
}

func (client *Client) EnqueueTurn(ctx context.Context, grant, idempotencyKey, sessionID, sourceRef, artifactID string) (domaincontrol.Turn, error) {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	protected, err := controlplaneclient.WithApplicationGrant(bounded, grant)
	if err != nil {
		return domaincontrol.Turn{}, err
	}
	response, err := client.client.ControlPlane.EnqueueTurn(protected, &controlplanev1.EnqueueTurnRequest{
		IdempotencyKey: idempotencyKey, SessionId: sessionID, SourceRef: sourceRef,
		PromptArtifactId: artifactID,
	})
	if err != nil || response.GetTurn() == nil || response.GetTurn().GetId() == "" {
		return domaincontrol.Turn{}, errors.New("enqueue control-plane turn")
	}
	return domaincontrol.Turn{ID: response.GetTurn().GetId(), Version: response.GetTurn().GetVersion()}, nil
}

func (client *Client) ClaimOwnerGate(ctx context.Context, idempotencyKey string) (entity.OwnerGateClaim, error) {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	response, err := client.client.ControlPlane.ClaimOwnerGateDelivery(bounded, &controlplanev1.ClaimOwnerGateDeliveryRequest{IdempotencyKey: idempotencyKey})
	if status.Code(err) == codes.NotFound {
		return entity.OwnerGateClaim{}, nil
	}
	if err != nil {
		return entity.OwnerGateClaim{}, errors.New("claim control-plane owner gate")
	}
	resource := response.GetOwnerGate()
	if resource == nil {
		return entity.OwnerGateClaim{}, nil
	}
	spec := resource.GetSpec().GetOwnerGate()
	if spec == nil || response.GetDeliveryClaimToken() == "" || response.GetDeliveryClaimExpiresAt() == nil ||
		spec.GetDeliveryFence() == 0 || spec.GetRecipientActorId() == "" || spec.GetExpiresAt() == nil ||
		spec.GetDeliveryId() == "" || spec.GetDeliveryPayloadSha256() == "" || spec.GetResultSha256() == "" ||
		resource.GetProjectId() == "" {
		return entity.OwnerGateClaim{}, errors.New("control-plane owner gate claim is incomplete")
	}
	return entity.OwnerGateClaim{
		DeliveryID:            spec.GetDeliveryId(),
		DeliveryPayloadSHA256: spec.GetDeliveryPayloadSha256(), GateID: resource.GetId(), GateVersion: resource.GetVersion(),
		ProjectID: resource.GetProjectId(), ProcessRunID: spec.GetProcessRunId(), SessionID: spec.GetSessionId(),
		TurnID: spec.GetTurnId(), Attempt: spec.GetAttempt(), ImmutableInputSHA256: spec.GetImmutableInputSha256(),
		RecipientActorID: spec.GetRecipientActorId(), ClaimToken: response.GetDeliveryClaimToken(),
		ClaimFence: spec.GetDeliveryFence(), ClaimExpiresAt: response.GetDeliveryClaimExpiresAt().AsTime(),
		ResultRef: spec.GetResultRef(), Summary: spec.GetResultSha256(),
	}, nil
}

func (client *Client) RecordOwnerGateDelivery(ctx context.Context, _ string, input domaincontrol.RecordDeliveryInput) error {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	response, err := client.client.ControlPlane.RecordOwnerGateDelivery(bounded, &controlplanev1.RecordOwnerGateDeliveryRequest{
		IdempotencyKey: input.IdempotencyKey, OwnerGateId: input.GateID, ExpectedVersion: input.GateVersion,
		DeliveryId: input.DeliveryID, DeliveryPayloadSha256: input.PayloadSHA256,
		DeliveryClaimToken: input.ClaimToken, DeliveryFence: input.ClaimFence,
		MattermostPostId: input.PostID, MattermostChannelId: input.ChannelID,
		MattermostRootPostId: input.RootPostID,
	})
	if err != nil || response.GetOwnerGate() == nil {
		return errors.New("record control-plane owner gate delivery")
	}
	return nil
}

func (client *Client) ResolveOwnerGate(ctx context.Context, grant string, input domaincontrol.ResolveGateInput) error {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	protected, err := controlplaneclient.WithApplicationGrant(bounded, grant)
	if err != nil {
		return err
	}
	processVersion := input.ProcessVersion
	if processVersion == 0 {
		read, readErr := client.client.ControlPlane.GetResource(protected, &controlplanev1.GetResourceRequest{
			ResourceId: input.ProcessRunID, ExpectedKind: controlplanev1.ResourceKind_RESOURCE_KIND_PROCESS_RUN,
		})
		if readErr != nil || read.GetResource() == nil || read.GetResource().GetVersion() == 0 {
			return errors.New("read control-plane process for owner decision")
		}
		processVersion = read.GetResource().GetVersion()
	}
	decision, ok := ownerDecision(input.Decision)
	if !ok {
		return errors.New("owner decision is invalid")
	}
	response, err := client.client.ControlPlane.ResolveOwnerGate(protected, &controlplanev1.ResolveOwnerGateRequest{
		IdempotencyKey: input.IdempotencyKey, OwnerGateId: input.GateID, ExpectedVersion: input.GateVersion,
		Decision: decision, Reason: input.Reason, ProcessRunId: input.ProcessRunID,
		ProcessExpectedVersion: processVersion, SessionId: input.SessionID, TurnId: input.TurnID,
		Attempt: input.Attempt, ImmutableInputSha256: input.ImmutableInputSHA256,
	})
	if err != nil {
		switch status.Code(err) {
		case codes.InvalidArgument, codes.PermissionDenied, codes.NotFound, codes.FailedPrecondition,
			codes.Aborted, codes.AlreadyExists:
			return domaincontrol.ErrConflict
		default:
			return errors.New("resolve control-plane owner gate")
		}
	}
	if response.GetOwnerGate() == nil || response.GetProcessRun() == nil {
		return errors.New("resolve control-plane owner gate")
	}
	return nil
}

func (client *Client) ExpireOwnerGate(ctx context.Context, idempotencyKey string) error {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	_, err := client.client.ControlPlane.ExpireOwnerGate(bounded, &controlplanev1.ExpireOwnerGateRequest{IdempotencyKey: idempotencyKey})
	if status.Code(err) == codes.NotFound {
		return nil
	}
	if err != nil {
		return errors.New("expire control-plane owner gate")
	}
	return nil
}

func (client *Client) ClaimInteractionDelivery(ctx context.Context, idempotencyKey string) (domaincontrol.InteractionDeliveryWork, error) {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	response, err := client.client.ControlPlane.ClaimInteractionDelivery(bounded,
		&controlplanev1.ClaimInteractionDeliveryRequest{IdempotencyKey: idempotencyKey})
	if status.Code(err) == codes.NotFound || response.GetDeliveryId() == "" {
		return domaincontrol.InteractionDeliveryWork{}, nil
	}
	if err != nil || response.GetDeliveryLeaseExpiresAt() == nil {
		return domaincontrol.InteractionDeliveryWork{}, errors.New("claim control-plane interaction delivery")
	}
	return domaincontrol.InteractionDeliveryWork{DeliveryID: response.GetDeliveryId(),
		OrganizationID: response.GetOrganizationId(), ProjectID: response.GetProjectId(), ActorID: response.GetActorId(),
		SessionID: response.GetSessionId(), SessionVersion: response.GetSessionVersion(), TurnID: response.GetTurnId(),
		TurnVersion: response.GetTurnVersion(), Attempt: response.GetAttempt(), RuntimeRevisionID: response.GetRuntimeRevisionId(),
		RuntimeRevisionVersion: response.GetRuntimeRevisionVersion(), ImmutableInputSHA256: response.GetImmutableInputSha256(),
		Kind: response.GetKind(), LifecycleState: stringWithoutPrefix(response.GetLifecycleState().String(), "RESOURCE_STATE_"),
		Outcome: response.GetOutcome(), ArtifactID: response.GetArtifactId(), ArtifactVersion: response.GetArtifactVersion(),
		ArtifactSHA256: response.GetArtifactSha256(), ArtifactName: response.GetArtifactName(),
		ArtifactStorageRef: response.GetArtifactStorageRef(), ArtifactSizeBytes: response.GetArtifactSizeBytes(),
		ArtifactMediaType: response.GetArtifactMediaType(), Fence: response.GetDeliveryFence(),
		LeaseToken: response.GetDeliveryLeaseToken(), LeaseExpiresAt: response.GetDeliveryLeaseExpiresAt().AsTime(),
		ReadbackCredential: response.GetDeliveryReadbackCredential(), InlinePayload: slices.Clone(response.GetInlinePayload())}, nil
}

func (client *Client) RecordInteractionDelivery(ctx context.Context, idempotencyKey string,
	work domaincontrol.InteractionDeliveryWork, providerReceiptSHA256 string) error {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	response, err := client.client.ControlPlane.RecordInteractionDelivery(bounded,
		&controlplanev1.RecordInteractionDeliveryRequest{IdempotencyKey: idempotencyKey,
			DeliveryId: work.DeliveryID, DeliveryFence: work.Fence, DeliveryLeaseToken: work.LeaseToken,
			ProviderReceiptSha256: providerReceiptSHA256})
	if err != nil || response.GetDeliveryId() != work.DeliveryID || response.GetProviderReceiptSha256() != providerReceiptSHA256 {
		return errors.New("record control-plane interaction delivery")
	}
	return nil
}

func (client *Client) IssueInteractionDeliveryReadback(ctx context.Context, idempotencyKey, deliveryID string,
	readiness bool) (string, time.Time, error) {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	response, err := client.client.ControlPlane.IssueInteractionDeliveryReadbackGrant(bounded,
		&controlplanev1.IssueInteractionDeliveryReadbackGrantRequest{IdempotencyKey: idempotencyKey,
			DeliveryId: deliveryID, Readiness: readiness})
	if err != nil || response.GetDeliveryId() != deliveryID || response.GetCredential() == "" || response.GetExpiresAt() == nil {
		return "", time.Time{}, errors.New("issue interaction delivery readback credential")
	}
	return response.GetCredential(), response.GetExpiresAt().AsTime(), nil
}

func (client *Client) ValidateInteractionDeliveryReadback(ctx context.Context, idempotencyKey, grantID, deliveryID,
	organizationID, projectID, credentialSHA256 string, generation uint64) (bool, error) {
	bounded, cancel := context.WithTimeout(ctx, client.deadline)
	defer cancel()
	response, err := client.client.ControlPlane.ValidateInteractionDeliveryReadbackGrant(bounded,
		&controlplanev1.ValidateInteractionDeliveryReadbackGrantRequest{IdempotencyKey: idempotencyKey,
			GrantId: grantID, DeliveryId: deliveryID, OrganizationId: organizationID, ProjectId: projectID,
			CredentialSha256: credentialSHA256, Generation: generation})
	if err != nil || response.GetGrantId() != grantID || response.GetDeliveryId() != deliveryID {
		return false, errors.New("validate interaction delivery readback credential")
	}
	return response.GetActive(), nil
}

func scanState(resource *controlplanev1.Resource) string {
	if resource == nil || resource.GetSpec().GetArtifact() == nil {
		return ""
	}
	return stringWithoutPrefix(resource.GetSpec().GetArtifact().GetScanStatus().String(), "ARTIFACT_SCAN_STATE_")
}

func projectArtifact(resource *controlplanev1.Resource) domaincontrol.Artifact {
	if resource == nil || resource.GetSpec().GetArtifact() == nil {
		return domaincontrol.Artifact{}
	}
	spec := resource.GetSpec().GetArtifact()
	return domaincontrol.Artifact{ID: resource.GetId(), Version: resource.GetVersion(), Name: resource.GetName(),
		ScanState: scanState(resource), Direction: spec.GetDirection(),
		StorageRef: spec.GetStorageRef(), SizeBytes: spec.GetSizeBytes(), MediaType: spec.GetMediaType(), SHA256: spec.GetSha256()}
}

func ownerDecision(value string) (controlplanev1.OwnerGateDecision, bool) {
	switch value {
	case "APPROVE":
		return controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_APPROVED, true
	case "REJECT":
		return controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_REJECTED, true
	case "CHANGES_REQUESTED":
		return controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_CHANGES_REQUESTED, true
	case "CANCEL":
		return controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_CANCELLED, true
	default:
		return 0, false
	}
}

func stringWithoutPrefix(value, prefix string) string {
	if len(value) >= len(prefix) && value[:len(prefix)] == prefix {
		return value[len(prefix):]
	}
	return value
}
