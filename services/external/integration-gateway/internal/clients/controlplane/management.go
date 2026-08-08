package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	controlplanecontract "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/managementeffect"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration/gitsource"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/security/effectreceipt"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ManagementClient struct {
	client *controlplaneclient.Client
	signer *effectreceipt.Signer
}

var managementReceiptNamespace = uuid.MustParse("0dd9eef1-4eaa-44d8-86d5-4e5443c5e1e5")

func stableReceiptID(parts ...string) string {
	return uuid.NewSHA1(managementReceiptNamespace, []byte(strings.Join(parts, "\x00"))).String()
}

func NewManagementClient(client *controlplaneclient.Client, signer *effectreceipt.Signer) (*ManagementClient, error) {
	if client == nil || signer == nil {
		return nil, errors.New("control-plane management client configuration is invalid")
	}
	return &ManagementClient{client: client, signer: signer}, nil
}

func (client *ManagementClient) resolveProviderReference(ctx context.Context, scope domainrepo.Scope, connection entity.ManagedProviderConnection, intentDigest string) (managementeffect.Readback, bool, error) {
	observation := sha256.Sum256([]byte(connection.ID + "\x00" + connection.ProviderID + "\x00archive-discovery\x00" + intentDigest))
	observationDigest := hex.EncodeToString(observation[:])
	request := &controlplanev1.ManageProviderConnectionReferenceRequest{
		Action: controlplanev1.ProviderConnectionReferenceAction_PROVIDER_CONNECTION_REFERENCE_ACTION_ARCHIVE,
		Name:   connection.DisplayName,
		Spec: &controlplanev1.ProviderConnectionReferenceSpec{StableKey: connection.StableKey, Provider: connection.ProviderID,
			ServerReference: connection.ID, ReferenceVersion: connection.Version + 1, ReferenceGeneration: connection.Generation,
			ReferenceSha256: observationDigest, MaskedLabel: connection.MaskedLabel, MaskedStatus: controlplanev1.ProviderConnectionStatus_PROVIDER_CONNECTION_STATUS_ARCHIVED},
	}
	authority := controlplanecontract.VerifiedCommandAuthority{ActorID: scope.ActorID, OrganizationID: scope.TenantID, ProjectID: scope.ProjectID, WorkloadID: "integration-gateway", FullMethod: controlplanev1.ControlPlaneService_ManageProviderConnectionReference_FullMethodName}
	commandIntent, err := controlplanecontract.ProviderConnectionReferenceIntentSHA256(authority, request)
	if err != nil {
		return managementeffect.Readback{}, false, err
	}
	maskedLabel := connection.MaskedLabel
	if maskedLabel == "" {
		maskedLabel = connection.DisplayName
	}
	credential, err := client.signer.SignProvider(effectreceipt.ProviderReceipt{
		FullMethod: controlplanev1.ControlPlaneService_ListProviderConnectionReferences_FullMethodName,
		ActorID:    scope.ActorID, OrganizationID: scope.TenantID, ProjectID: scope.ProjectID,
		Action: "archive", Effect: "provider-reference-discovery", EffectVersion: connection.Version + 1,
		EffectGeneration: connection.Generation, EffectSHA256: observationDigest, ReceiptID: uuid.NewString(),
		ReceiptRevision: connection.Version + 1, ProviderObjectRef: connection.ID, MaskedStatus: "ARCHIVED",
		Provider: connection.ProviderID, MaskedLabel: maskedLabel, TargetKind: "provider_connection_reference",
		TargetStableKey: connection.StableKey, CommandIntentSHA256: commandIntent,
	})
	if err != nil {
		return managementeffect.Readback{}, false, err
	}
	requestContext, err := controlplaneclient.WithApplicationGrant(ctx, credential.CompactJWS)
	if err != nil {
		return managementeffect.Readback{}, false, err
	}
	response, err := client.client.ControlPlane.ListProviderConnectionReferences(requestContext, &controlplanev1.ListProviderConnectionReferencesRequest{PageSize: 100})
	if err != nil {
		return managementeffect.Readback{}, false, errors.Join(managementeffect.ErrOutcomeUnknown, err)
	}
	if response.GetNextPageToken() != "" {
		return managementeffect.Readback{}, false, errors.New("control-plane provider reference discovery is not bounded")
	}
	var found *controlplanev1.Resource
	for _, candidate := range response.GetProviderConnectionReferences() {
		spec := candidate.GetSpec().GetProviderConnectionReference()
		if spec != nil && spec.GetStableKey() == connection.StableKey && spec.GetServerReference() == connection.ID {
			if found != nil {
				return managementeffect.Readback{}, false, errors.New("control-plane provider reference discovery is ambiguous")
			}
			found = candidate
		}
	}
	if found == nil {
		return managementeffect.Readback{}, false, nil
	}
	if found.GetId() == "" || found.GetVersion() == 0 || !validDigest(found.GetProjectionSha256()) {
		return managementeffect.Readback{}, false, errors.New("control-plane provider reference discovery readback is invalid")
	}
	return resourceReadback(found), true, nil
}

func (client *ManagementClient) SyncProvider(ctx context.Context, scope domainrepo.Scope, connection entity.ManagedProviderConnection, credential entity.CredentialGeneration, intentDigest string) (managementeffect.Readback, error) {
	action := "register"
	protoAction := controlplanev1.ProviderConnectionReferenceAction_PROVIDER_CONNECTION_REFERENCE_ACTION_REGISTER
	maskedStatus := "AVAILABLE"
	eligible := true
	if connection.ControlPlaneID != "" {
		action = "refresh"
		protoAction = controlplanev1.ProviderConnectionReferenceAction_PROVIDER_CONNECTION_REFERENCE_ACTION_REFRESH
	}
	if connection.Status == "REVOKED" {
		action = "archive"
		protoAction = controlplanev1.ProviderConnectionReferenceAction_PROVIDER_CONNECTION_REFERENCE_ACTION_ARCHIVE
		maskedStatus = "ARCHIVED"
		eligible = false
	}
	if connection.Status == "REVOKED" && connection.ControlPlaneID == "" {
		resolved, found, resolveErr := client.resolveProviderReference(ctx, scope, connection, intentDigest)
		if resolveErr != nil {
			return managementeffect.Readback{}, resolveErr
		}
		if !found {
			return managementeffect.Readback{}, nil
		}
		connection.ControlPlaneID, connection.ControlPlaneVersion, connection.ControlPlaneDigest = resolved.ResourceID, resolved.Version, resolved.Digest
	}
	observation := sha256.Sum256([]byte(connection.ID + "\x00" + connection.ProviderID + "\x00" + connection.CredentialBindingDigest + "\x00" + intentDigest))
	observationDigest := hex.EncodeToString(observation[:])
	idempotencyKey := stableReceiptID(scope.TenantID, scope.ProjectID, connection.ID, action, intentDigest)
	status := controlplanev1.ProviderConnectionStatus_PROVIDER_CONNECTION_STATUS_AVAILABLE
	if !eligible {
		status = controlplanev1.ProviderConnectionStatus_PROVIDER_CONNECTION_STATUS_ARCHIVED
	}
	observedAt := connection.Capacity.ObservedAt
	if observedAt.IsZero() {
		observedAt = connection.UpdatedAt
	}
	request := &controlplanev1.ManageProviderConnectionReferenceRequest{IdempotencyKey: idempotencyKey, Action: protoAction, ProviderConnectionReferenceId: connection.ControlPlaneID, ExpectedVersion: connection.ControlPlaneVersion, Name: connection.DisplayName, Spec: &controlplanev1.ProviderConnectionReferenceSpec{StableKey: connection.StableKey, Provider: connection.ProviderID, ServerReference: connection.ID, ReferenceVersion: connection.Version + 1, ReferenceSha256: observationDigest, MaskedLabel: connection.MaskedLabel, MaskedStatus: status, Capabilities: connection.Capabilities, Eligible: eligible, ObservedAt: timestamppb.New(observedAt), CredentialBindingId: connection.CredentialBindingID, CredentialBindingVersion: connection.CredentialBindingVersion, CredentialBindingSha256: connection.CredentialBindingDigest, ReferenceGeneration: connection.Generation,
		ObservedUsage: connection.Capacity.Usage, ObservedLimit: connection.Capacity.Limit,
		ObservationRevision: connection.Capacity.Revision, ObservationExpiresAt: optionalReceiptTime(connection.Capacity.ExpiresAt),
		WindowDurationSeconds: connection.Capacity.WindowSeconds, ResetsAt: optionalReceiptTime(connection.Capacity.ResetsAt),
		ObservationSha256: connection.Capacity.Digest}}
	authority := controlplanecontract.VerifiedCommandAuthority{ActorID: scope.ActorID, OrganizationID: scope.TenantID, ProjectID: scope.ProjectID, WorkloadID: "integration-gateway", FullMethod: controlplanev1.ControlPlaneService_ManageProviderConnectionReference_FullMethodName}
	commandIntent, err := controlplanecontract.ProviderConnectionReferenceIntentSHA256(authority, request)
	if err != nil {
		return managementeffect.Readback{}, err
	}
	baseReceipt := effectreceipt.ProviderReceipt{ActorID: scope.ActorID, OrganizationID: scope.TenantID, ProjectID: scope.ProjectID, Action: action, Effect: "provider_connection_reference", EffectVersion: connection.Version + 1, EffectGeneration: connection.Generation, EffectSHA256: observationDigest, ReceiptRevision: connection.Version + 1, CredentialBindingID: connection.CredentialBindingID, CredentialBindingVersion: connection.CredentialBindingVersion, CredentialBindingSHA256: connection.CredentialBindingDigest, ProviderObjectRef: connection.ID, MaskedStatus: maskedStatus, Provider: connection.ProviderID, MaskedLabel: connection.MaskedLabel, Capabilities: connection.Capabilities, Eligible: eligible, TargetKind: "provider_connection_reference", TargetResourceID: connection.ControlPlaneID, TargetStableKey: connection.StableKey, CommandIntentSHA256: commandIntent,
		SecretRef: credential.SecretRef, SecretVersion: credential.SecretVersion, SecretContentSHA256: credential.SecretContentDigest,
		MaskedAccount: credential.MaskedAccount, ObservedUsage: credential.Capacity.Usage, ObservedLimit: credential.Capacity.Limit,
		ObservationRevision: credential.Capacity.Revision, ObservedAt: credential.Capacity.ObservedAt,
		WindowDurationSeconds: credential.Capacity.WindowSeconds, ResetsAt: credential.Capacity.ResetsAt,
		ObservationExpiresAt: credential.Capacity.ExpiresAt}
	baseReceipt.ObservationSHA256 = credential.Capacity.Digest
	manageReceipt := baseReceipt
	manageReceipt.FullMethod, manageReceipt.ReceiptID = authority.FullMethod, uuid.NewString()
	signedCredential, err := client.signer.SignProvider(manageReceipt)
	if err != nil {
		return managementeffect.Readback{}, err
	}
	requestContext, err := controlplaneclient.WithApplicationGrant(ctx, signedCredential.CompactJWS)
	if err != nil {
		return managementeffect.Readback{}, err
	}
	receipt := providerReceiptProto(signedCredential.Receipt)
	request.ProviderReceipt = receipt
	response, err := client.client.ControlPlane.ManageProviderConnectionReference(requestContext, request)
	if err != nil {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, err)
	}
	resource := response.GetProviderConnectionReference()
	if resource == nil || resource.GetId() == "" || resource.GetVersion() == 0 || !validDigest(resource.GetProjectionSha256()) {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, errors.New("control-plane provider readback is incomplete"))
	}
	getReceipt := baseReceipt
	getReceipt.FullMethod, getReceipt.ReceiptID, getReceipt.TargetResourceID = controlplanev1.ControlPlaneService_GetProviderConnectionReference_FullMethodName, uuid.NewString(), resource.GetId()
	getCredential, err := client.signer.SignProvider(getReceipt)
	if err != nil {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, err)
	}
	getContext, err := controlplaneclient.WithApplicationGrant(ctx, getCredential.CompactJWS)
	if err != nil {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, err)
	}
	get, err := client.client.ControlPlane.GetProviderConnectionReference(getContext, &controlplanev1.GetProviderConnectionReferenceRequest{ProviderConnectionReferenceId: resource.GetId()})
	if err != nil {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, err)
	}
	listReceipt := baseReceipt
	listReceipt.FullMethod, listReceipt.ReceiptID, listReceipt.TargetResourceID = controlplanev1.ControlPlaneService_ListProviderConnectionReferences_FullMethodName, uuid.NewString(), resource.GetId()
	listCredential, err := client.signer.SignProvider(listReceipt)
	if err != nil {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, err)
	}
	listContext, err := controlplaneclient.WithApplicationGrant(ctx, listCredential.CompactJWS)
	if err != nil {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, err)
	}
	listed, err := client.client.ControlPlane.ListProviderConnectionReferences(listContext, &controlplanev1.ListProviderConnectionReferencesRequest{PageSize: 100})
	if err != nil {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, err)
	}
	found := false
	for _, candidate := range listed.GetProviderConnectionReferences() {
		if candidate.GetId() == resource.GetId() && candidate.GetVersion() == resource.GetVersion() && candidate.GetProjectionSha256() == resource.GetProjectionSha256() {
			found = true
			break
		}
	}
	if !found || get.GetProviderConnectionReference().GetVersion() != resource.GetVersion() ||
		get.GetProviderConnectionReference().GetProjectionSha256() != resource.GetProjectionSha256() {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, errors.New("control-plane provider version-pinned readback mismatch"))
	}
	storedSpec := get.GetProviderConnectionReference().GetSpec().GetProviderConnectionReference()
	if action != "archive" && (storedSpec == nil || storedSpec.GetCredentialBindingId() != credential.CredentialBindingID ||
		storedSpec.GetCredentialBindingVersion() != credential.CredentialBindingVersion || !validDigest(storedSpec.GetCredentialBindingSha256())) {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, errors.New("control-plane credential binding typed readback mismatch"))
	}
	result := resourceReadback(resource)
	if storedSpec != nil {
		result.CredentialBindingID, result.CredentialBindingVersion, result.CredentialBindingDigest = storedSpec.GetCredentialBindingId(), storedSpec.GetCredentialBindingVersion(), storedSpec.GetCredentialBindingSha256()
	}
	return result, nil
}

func providerReceiptProto(value effectreceipt.ProviderReceipt) *controlplanev1.ProviderEffectReadbackReceipt {
	return &controlplanev1.ProviderEffectReadbackReceipt{ContractVersion: value.ContractVersion, Issuer: value.Issuer, Purpose: value.Purpose, WorkloadId: value.WorkloadID, CallerSpiffeId: value.CallerSPIFFEID, FullMethod: value.FullMethod, ActorId: value.ActorID, OrganizationId: value.OrganizationID, ProjectId: value.ProjectID, ProviderObjectRef: value.ProviderObjectRef, Action: value.Action, Effect: value.Effect, EffectVersion: value.EffectVersion, EffectGeneration: value.EffectGeneration, EffectSha256: value.EffectSHA256, ReceiptId: value.ReceiptID, ReceiptRevision: value.ReceiptRevision, IssuedAt: timestamppb.New(value.IssuedAt), NotBefore: timestamppb.New(value.NotBefore), ExpiresAt: timestamppb.New(value.ExpiresAt), CredentialBindingId: value.CredentialBindingID, CredentialBindingVersion: value.CredentialBindingVersion, CredentialBindingSha256: value.CredentialBindingSHA256, ProviderUsername: value.ProviderUsername, MaskedStatus: value.MaskedStatus, Provider: value.Provider, MaskedLabel: value.MaskedLabel, Capabilities: value.Capabilities, Eligible: value.Eligible, TargetKind: value.TargetKind, TargetResourceId: value.TargetResourceID, TargetStableKey: value.TargetStableKey, CommandIntentSha256: value.CommandIntentSHA256,
		SecretRef: value.SecretRef, SecretVersion: value.SecretVersion, SecretContentSha256: value.SecretContentSHA256,
		MaskedAccount: value.MaskedAccount, ObservedUsage: value.ObservedUsage, ObservedLimit: value.ObservedLimit,
		ObservationRevision: value.ObservationRevision, ObservedAt: optionalReceiptTime(value.ObservedAt),
		WindowDurationSeconds: value.WindowDurationSeconds, ResetsAt: optionalReceiptTime(value.ResetsAt),
		ObservationExpiresAt: optionalReceiptTime(value.ObservationExpiresAt), ObservationSha256: value.ObservationSHA256}
}

func optionalReceiptTime(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func resourceReadback(value *controlplanev1.Resource) managementeffect.Readback {
	return managementeffect.Readback{ResourceID: value.GetId(), Version: value.GetVersion(), Digest: value.GetProjectionSha256()}
}

func (client *ManagementClient) SyncPool(ctx context.Context, scope domainrepo.Scope, pool entity.ManagedProviderPool, intentDigest string) (managementeffect.Readback, error) {
	action := controlplanev1.ProviderPoolAction_PROVIDER_POOL_ACTION_CREATE
	actionName := "create"
	if pool.ControlPlaneID != "" {
		action = controlplanev1.ProviderPoolAction_PROVIDER_POOL_ACTION_UPDATE
		actionName = "update"
	}
	if pool.Status == "ARCHIVED" {
		action = controlplanev1.ProviderPoolAction_PROVIDER_POOL_ACTION_ARCHIVE
		actionName = "archive"
	}
	if pool.Status == "DELETED" {
		action = controlplanev1.ProviderPoolAction_PROVIDER_POOL_ACTION_DELETE
		actionName = "delete"
	}
	bindings := make([]*controlplanev1.ProviderPoolBinding, 0, len(pool.Members))
	for _, member := range pool.Members {
		if member.ControlPlaneID == "" || member.ControlPlaneVersion == 0 || member.ControlPlaneDigest == "" {
			return managementeffect.Readback{}, errors.New("provider pool member version-pinned readback mismatch")
		}
		if member.Capacity.Limit == 0 || member.Capacity.Usage > member.Capacity.Limit || member.Capacity.Revision == 0 ||
			member.Capacity.ObservedAt.IsZero() || !member.Capacity.ExpiresAt.After(time.Now().UTC()) || !validDigest(member.Capacity.Digest) {
			return managementeffect.Readback{}, errors.New("provider pool capacity observation is stale")
		}
		bindings = append(bindings, &controlplanev1.ProviderPoolBinding{ProviderConnectionReferenceId: member.ControlPlaneID, ReferenceVersion: member.ControlPlaneVersion, ReferenceSha256: member.ControlPlaneDigest, Weight: member.Weight, Eligible: member.Eligible, MaskedStatus: controlplanev1.ProviderConnectionStatus_PROVIDER_CONNECTION_STATUS_AVAILABLE, ProviderConnectionStableKey: member.ConnectionStableKey,
			ObservedUsage: member.Capacity.Usage, ObservedLimit: member.Capacity.Limit,
			ObservationRevision: member.Capacity.Revision, ObservedAt: timestamppb.New(member.Capacity.ObservedAt),
			ObservationExpiresAt: timestamppb.New(member.Capacity.ExpiresAt), ObservationSha256: member.Capacity.Digest,
			WindowDurationSeconds: member.Capacity.WindowSeconds, ResetsAt: timestamppb.New(member.Capacity.ResetsAt)})
	}
	sort.Slice(bindings, func(left, right int) bool {
		return bindings[left].ProviderConnectionReferenceId < bindings[right].ProviderConnectionReferenceId
	})
	observation := sha256.Sum256([]byte(pool.ID + "\x00" + pool.EffectiveDigest + "\x00" + intentDigest))
	receiptID := stableReceiptID(scope.TenantID, scope.ProjectID, pool.ID, actionName, intentDigest)
	request := &controlplanev1.ManageProviderPoolRequest{IdempotencyKey: receiptID, Action: action, ProviderPoolId: pool.ControlPlaneID, ExpectedVersion: pool.ControlPlaneVersion, Name: pool.DisplayName, Spec: &controlplanev1.ProviderPoolSpec{StableKey: pool.StableKey, Policy: pool.Policy, PolicyRevision: pool.Version, ObservationMaxAge: durationpb.New(5 * time.Minute), Bindings: bindings, EligibilitySnapshotSha256: pool.EffectiveDigest, Ownership: &controlplanev1.ConfigurationOwnership{ManagedBy: controlplanev1.ConfigurationManager_CONFIGURATION_MANAGER_UI}}}
	authority := controlplanecontract.VerifiedCommandAuthority{ActorID: scope.ActorID, OrganizationID: scope.TenantID, ProjectID: scope.ProjectID, WorkloadID: "integration-gateway", FullMethod: controlplanev1.ControlPlaneService_ManageProviderPool_FullMethodName}
	commandIntent, err := controlplanecontract.ProviderPoolIntentSHA256(authority, request)
	if err != nil {
		return managementeffect.Readback{}, err
	}
	credential, err := client.signer.SignProvider(effectreceipt.ProviderReceipt{FullMethod: authority.FullMethod, ActorID: scope.ActorID, OrganizationID: scope.TenantID, ProjectID: scope.ProjectID, Action: actionName, Effect: "pool-observation", EffectVersion: pool.Version, EffectGeneration: pool.ObservationVersion, EffectSHA256: hex.EncodeToString(observation[:]), ReceiptID: receiptID, ReceiptRevision: pool.Version, MaskedStatus: pool.Status, Provider: "provider-pool", MaskedLabel: pool.DisplayName, Eligible: pool.Status == "ACTIVE", TargetKind: "provider_pool", TargetResourceID: pool.ControlPlaneID, TargetStableKey: pool.StableKey, CommandIntentSHA256: commandIntent})
	if err != nil {
		return managementeffect.Readback{}, err
	}
	requestContext, err := controlplaneclient.WithApplicationGrant(ctx, credential.CompactJWS)
	if err != nil {
		return managementeffect.Readback{}, err
	}
	request.ProviderReceipt = providerReceiptProto(credential.Receipt)
	response, err := client.client.ControlPlane.ManageProviderPool(requestContext, request)
	if err != nil {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, err)
	}
	resource := response.GetProviderPool()
	if resource == nil || resource.GetId() == "" || resource.GetVersion() == 0 || !validDigest(resource.GetProjectionSha256()) {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, errors.New("control-plane provider pool readback is absent"))
	}
	return resourceReadback(resource), nil
}
func (client *ManagementClient) Check(ctx context.Context) error { return client.client.Check(ctx) }

type preparedGitReconciliation struct {
	document     gitsource.Document
	request      proto.Message
	fullMethod   string
	targetKind   string
	intentSHA256 string
}

func prepareGitReconciliation(scope domainrepo.Scope, binding entity.GitSourceBinding, reconciliation entity.GitReconciliation, snapshot []byte, sourceRef string) (preparedGitReconciliation, error) {
	document, err := gitsource.ParseDocument(snapshot, binding.TargetKind, binding.TargetStableKey, sourceRef, reconciliation.SourceRevision, reconciliation.SourceDigest)
	if err != nil {
		return preparedGitReconciliation{}, err
	}
	fullMethod := map[string]string{"ROLE_DEFINITION": controlplanev1.ControlPlaneService_ReconcileGitRoleDefinition_FullMethodName, "AGENT": controlplanev1.ControlPlaneService_ReconcileGitAgent_FullMethodName, "INSTRUCTION_SET": controlplanev1.ControlPlaneService_ReconcileGitInstructionSet_FullMethodName, "PROVIDER_POOL": controlplanev1.ControlPlaneService_ReconcileGitProviderPool_FullMethodName}[binding.TargetKind]
	targetKind := map[string]string{"ROLE_DEFINITION": "role_definition", "AGENT": "agent", "INSTRUCTION_SET": "instruction_set", "PROVIDER_POOL": "provider_pool"}[binding.TargetKind]
	var request proto.Message
	switch binding.TargetKind {
	case "ROLE_DEFINITION":
		request = &controlplanev1.ReconcileGitRoleDefinitionRequest{IdempotencyKey: reconciliation.ReceiptID, RoleDefinitionId: document.ResourceID, ExpectedVersion: document.ExpectedVersion, Name: document.Name, Spec: document.Role}
	case "AGENT":
		request = &controlplanev1.ReconcileGitAgentRequest{IdempotencyKey: reconciliation.ReceiptID, AgentId: document.ResourceID, ExpectedVersion: document.ExpectedVersion, Name: document.Name, Spec: document.Agent, RoleDefinitionStableKey: document.RoleStableKey, InstructionSetStableKey: document.InstructionSetStableKey, ProviderPoolStableKey: document.ProviderPoolStableKey}
	case "INSTRUCTION_SET":
		request = &controlplanev1.ReconcileGitInstructionSetRequest{IdempotencyKey: reconciliation.ReceiptID, InstructionSetId: document.ResourceID, ExpectedVersion: document.ExpectedVersion, Name: document.Name, Spec: document.InstructionSet}
	case "PROVIDER_POOL":
		request = &controlplanev1.ReconcileGitProviderPoolRequest{IdempotencyKey: reconciliation.ReceiptID, ProviderPoolId: document.ResourceID, ExpectedVersion: document.ExpectedVersion, Name: document.Name, Spec: document.ProviderPool}
	default:
		return preparedGitReconciliation{}, errors.New("Git reconciliation target is unsupported")
	}
	authority := controlplanecontract.VerifiedCommandAuthority{ActorID: scope.ActorID, OrganizationID: scope.TenantID, ProjectID: scope.ProjectID, WorkloadID: "integration-gateway", FullMethod: fullMethod}
	commandIntent, err := controlplanecontract.GitReconciliationIntentSHA256(authority, request)
	if err != nil {
		return preparedGitReconciliation{}, err
	}
	return preparedGitReconciliation{document: document, request: request, fullMethod: fullMethod, targetKind: targetKind, intentSHA256: commandIntent}, nil
}

func (client *ManagementClient) GitIntentSHA256(scope domainrepo.Scope, binding entity.GitSourceBinding, reconciliation entity.GitReconciliation, snapshot []byte, sourceRef string) (string, error) {
	prepared, err := prepareGitReconciliation(scope, binding, reconciliation, snapshot, sourceRef)
	if err != nil {
		return "", err
	}
	return prepared.intentSHA256, nil
}

func (client *ManagementClient) ReconcileGit(ctx context.Context, scope domainrepo.Scope, binding entity.GitSourceBinding, reconciliation entity.GitReconciliation, snapshot []byte, sourceRef string) (managementeffect.Readback, error) {
	prepared, err := prepareGitReconciliation(scope, binding, reconciliation, snapshot, sourceRef)
	if err != nil {
		return managementeffect.Readback{}, err
	}
	if reconciliation.CommandIntentDigest != prepared.intentSHA256 {
		return managementeffect.Readback{}, errors.New("Git reconciliation semantic intent mismatch")
	}
	credential, err := client.signer.SignGit(effectreceipt.GitReceipt{FullMethod: prepared.fullMethod, ActorID: scope.ActorID, OrganizationID: scope.TenantID, ProjectID: scope.ProjectID, TargetKind: prepared.targetKind, TargetResourceID: prepared.document.ResourceID, TargetStableKey: binding.TargetStableKey, SourceRef: sourceRef, SourceRevision: reconciliation.SourceRevision, SourceSHA256: reconciliation.SourceDigest, CommandIntentSHA256: prepared.intentSHA256, ReceiptID: reconciliation.ReceiptID, ReceiptRevision: reconciliation.SourceRevision})
	if err != nil {
		return managementeffect.Readback{}, err
	}
	requestContext, err := controlplaneclient.WithApplicationGrant(ctx, credential.CompactJWS)
	if err != nil {
		return managementeffect.Readback{}, err
	}
	receipt := gitReceiptProto(credential.Receipt)
	var resource *controlplanev1.Resource
	switch value := prepared.request.(type) {
	case *controlplanev1.ReconcileGitRoleDefinitionRequest:
		value.ReconciliationReceipt = receipt
		response, callErr := client.client.ControlPlane.ReconcileGitRoleDefinition(requestContext, value)
		if callErr != nil {
			return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, callErr)
		}
		resource = response.GetRoleDefinition()
	case *controlplanev1.ReconcileGitAgentRequest:
		value.ReconciliationReceipt = receipt
		response, callErr := client.client.ControlPlane.ReconcileGitAgent(requestContext, value)
		if callErr != nil {
			return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, callErr)
		}
		resource = response.GetAgent()
	case *controlplanev1.ReconcileGitInstructionSetRequest:
		value.ReconciliationReceipt = receipt
		response, callErr := client.client.ControlPlane.ReconcileGitInstructionSet(requestContext, value)
		if callErr != nil {
			return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, callErr)
		}
		resource = response.GetInstructionSet()
	case *controlplanev1.ReconcileGitProviderPoolRequest:
		value.ReconciliationReceipt = receipt
		response, callErr := client.client.ControlPlane.ReconcileGitProviderPool(requestContext, value)
		if callErr != nil {
			return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, callErr)
		}
		resource = response.GetProviderPool()
	default:
		return managementeffect.Readback{}, errors.New("Git reconciliation target is unsupported")
	}
	if resource == nil || resource.GetId() == "" || resource.GetVersion() == 0 || !validDigest(resource.GetProjectionSha256()) {
		return managementeffect.Readback{}, errors.Join(managementeffect.ErrOutcomeUnknown, errors.New("Git reconciliation readback is incomplete"))
	}
	return resourceReadback(resource), nil
}

func gitReceiptProto(value effectreceipt.GitReceipt) *controlplanev1.GitReconciliationReceipt {
	return &controlplanev1.GitReconciliationReceipt{ContractVersion: value.ContractVersion, Issuer: value.Issuer, Purpose: value.Purpose, WorkloadId: value.WorkloadID, CallerSpiffeId: value.CallerSPIFFEID, FullMethod: value.FullMethod, ActorId: value.ActorID, OrganizationId: value.OrganizationID, ProjectId: value.ProjectID, TargetKind: value.TargetKind, TargetResourceId: value.TargetResourceID, TargetStableKey: value.TargetStableKey, SourceRef: value.SourceRef, SourceRevision: value.SourceRevision, SourceSha256: value.SourceSHA256, CommandIntentSha256: value.CommandIntentSHA256, ReceiptId: value.ReceiptID, ReceiptRevision: value.ReceiptRevision, IssuedAt: timestamppb.New(value.IssuedAt), NotBefore: timestamppb.New(value.NotBefore), ExpiresAt: timestamppb.New(value.ExpiresAt)}
}
