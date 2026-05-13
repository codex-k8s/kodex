package casters

import (
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	providerservice "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

// ProviderOperationResponse maps a provider write result to gRPC.
func ProviderOperationResponse(result providerservice.ProviderOperationResult) *providersv1.ProviderOperationResponse {
	response := &providersv1.ProviderOperationResponse{
		Result: &providersv1.ProviderOperationCommandResult{
			ResultRef:              optionalStringPtr(result.Result.ResultRef),
			ProviderObjectId:       optionalStringPtr(result.Result.ProviderObjectID),
			ProviderVersion:        optionalStringPtr(result.Result.ProviderVersion),
			ReconciliationEnqueued: result.Result.ReconciliationEnqueued,
			EmittedEventTypes:      append([]string(nil), result.Result.EmittedEventTypes...),
		},
	}
	if result.ProviderOperation != nil {
		response.ProviderOperation = ProviderOperationToProto(*result.ProviderOperation)
	}
	if result.Result.Target != nil {
		response.Result.Target = ProviderTargetToProto(*result.Result.Target)
	}
	if result.WorkItemProjection != nil {
		response.WorkItemProjection = WorkItemProjectionToProto(*result.WorkItemProjection)
	}
	if result.CommentProjection != nil {
		response.CommentProjection = CommentProjectionToProto(*result.CommentProjection)
	}
	if result.Relationship != nil {
		response.Relationship = RelationshipToProto(*result.Relationship)
	}
	return response
}

// ProviderTargetToProto maps a provider write target to gRPC.
func ProviderTargetToProto(target providerservice.ProviderTarget) *providersv1.ProviderTarget {
	return providerTargetMessage(
		target.ProviderSlug,
		target.RepositoryFullName,
		target.ProviderRepositoryID,
		target.WorkItemKind,
		target.Number,
		target.ProviderObjectID,
		target.WebURL,
	)
}

// PolicyContextToProto maps safe policy metadata to gRPC.
func PolicyContextToProto(policy value.ProviderOperationPolicyContext) *providersv1.ProviderOperationPolicyContext {
	if isEmptyPolicyContext(policy) {
		return nil
	}
	return &providersv1.ProviderOperationPolicyContext{
		ProjectId:         optionalStringPtr(policy.ProjectID),
		RepositoryId:      optionalStringPtr(policy.RepositoryID),
		Stage:             optionalStringPtr(policy.Stage),
		RoleId:            optionalStringPtr(policy.RoleID),
		RoleKey:           optionalStringPtr(policy.RoleKey),
		OperationType:     OperationTypeToProto(enum.ProviderOperationType(policy.OperationType)),
		TargetRef:         optionalStringPtr(policy.TargetRef),
		ChangedFields:     append([]string(nil), policy.ChangedFields...),
		RiskTags:          append([]string(nil), policy.RiskTags...),
		RiskLevel:         OperationRiskLevelToProto(policy.RiskLevel),
		ApprovalRequired:  policy.ApprovalRequired,
		PolicyVersion:     optionalStringPtr(policy.PolicyVersion),
		PolicySnapshotRef: optionalStringPtr(policy.PolicySnapshotRef),
	}
}

// ApprovalGateRefToProto maps safe approval metadata to gRPC.
func ApprovalGateRefToProto(reference value.ApprovalGateReference) *providersv1.ApprovalGateReference {
	if isEmptyApprovalGateRef(reference) {
		return nil
	}
	return &providersv1.ApprovalGateReference{
		ApprovalId:       reference.ApprovalID,
		GateType:         reference.GateType,
		Decision:         reference.Decision,
		DecidedByActorId: optionalStringPtr(reference.DecidedByActorID),
		DecidedAt:        timePtrString(reference.DecidedAt),
		EvidenceRef:      optionalStringPtr(reference.EvidenceRef),
		PolicyVersion:    optionalStringPtr(reference.PolicyVersion),
	}
}

func isEmptyPolicyContext(policy value.ProviderOperationPolicyContext) bool {
	return policy.ProjectID == "" &&
		policy.RepositoryID == "" &&
		policy.Stage == "" &&
		policy.RoleID == "" &&
		policy.RoleKey == "" &&
		policy.OperationType == "" &&
		policy.TargetRef == "" &&
		len(policy.ChangedFields) == 0 &&
		len(policy.RiskTags) == 0 &&
		policy.RiskLevel == "" &&
		!policy.ApprovalRequired &&
		policy.PolicyVersion == "" &&
		policy.PolicySnapshotRef == ""
}

func isEmptyApprovalGateRef(reference value.ApprovalGateReference) bool {
	return reference.ApprovalID == "" &&
		reference.GateType == "" &&
		reference.Decision == "" &&
		reference.DecidedByActorID == "" &&
		reference.DecidedAt == nil &&
		reference.EvidenceRef == "" &&
		reference.PolicyVersion == ""
}
