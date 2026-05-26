// Package enum contains governance-manager enum-like domain values.
package enum

// Operation identifies a GovernanceManagerService operation without coupling the domain to protobuf.
type Operation string

const (
	OperationCreateRiskProfile           Operation = "CreateRiskProfile"
	OperationCreateRiskProfileVersion    Operation = "CreateRiskProfileVersion"
	OperationActivateRiskProfileVersion  Operation = "ActivateRiskProfileVersion"
	OperationArchiveRiskProfile          Operation = "ArchiveRiskProfile"
	OperationGetRiskProfile              Operation = "GetRiskProfile"
	OperationGetRiskProfileVersion       Operation = "GetRiskProfileVersion"
	OperationListRiskProfiles            Operation = "ListRiskProfiles"
	OperationListRiskRules               Operation = "ListRiskRules"
	OperationListGatePolicies            Operation = "ListGatePolicies"
	OperationEvaluateRisk                Operation = "EvaluateRisk"
	OperationReevaluateRisk              Operation = "ReevaluateRisk"
	OperationGetRiskAssessment           Operation = "GetRiskAssessment"
	OperationListRiskAssessments         Operation = "ListRiskAssessments"
	OperationListRiskFactors             Operation = "ListRiskFactors"
	OperationRecordReviewSignal          Operation = "RecordReviewSignal"
	OperationListReviewSignals           Operation = "ListReviewSignals"
	OperationRequestGate                 Operation = "RequestGate"
	OperationSubmitGateDecision          Operation = "SubmitGateDecision"
	OperationCancelGate                  Operation = "CancelGate"
	OperationExpireGate                  Operation = "ExpireGate"
	OperationGetGateDecision             Operation = "GetGateDecision"
	OperationListGateDecisions           Operation = "ListGateDecisions"
	OperationGetGateRequest              Operation = "GetGateRequest"
	OperationListGateRequests            Operation = "ListGateRequests"
	OperationBuildReleaseDecisionPackage Operation = "BuildReleaseDecisionPackage"
	OperationGetReleaseDecisionPackage   Operation = "GetReleaseDecisionPackage"
	OperationListReleaseDecisionPackages Operation = "ListReleaseDecisionPackages"
	OperationRequestReleaseDecision      Operation = "RequestReleaseDecision"
	OperationSubmitReleaseDecision       Operation = "SubmitReleaseDecision"
	OperationGetReleaseDecision          Operation = "GetReleaseDecision"
	OperationListReleaseDecisions        Operation = "ListReleaseDecisions"
	OperationRecordBlockingSignal        Operation = "RecordBlockingSignal"
	OperationResolveBlockingSignal       Operation = "ResolveBlockingSignal"
	OperationListBlockingSignals         Operation = "ListBlockingSignals"
	OperationRecordReleaseSafetyState    Operation = "RecordReleaseSafetyState"
	OperationGetReleaseSafetyState       Operation = "GetReleaseSafetyState"
)

// String returns the stable operation name.
func (operation Operation) String() string {
	return string(operation)
}
