package enum

// RiskClass is the governance risk class used by stored assessments.
type RiskClass string

const (
	RiskClassUnspecified RiskClass = ""
	RiskClassR0          RiskClass = "R0"
	RiskClassR1          RiskClass = "R1"
	RiskClassR2          RiskClass = "R2"
	RiskClassR3          RiskClass = "R3"
)

// RiskProfileStatus is the lifecycle status of a risk profile.
type RiskProfileStatus string

const (
	RiskProfileStatusDraft    RiskProfileStatus = "draft"
	RiskProfileStatusActive   RiskProfileStatus = "active"
	RiskProfileStatusDisabled RiskProfileStatus = "disabled"
	RiskProfileStatusArchived RiskProfileStatus = "archived"
)

// RiskProfileVersionStatus is the lifecycle status of an immutable profile version.
type RiskProfileVersionStatus string

const (
	RiskProfileVersionStatusDraft      RiskProfileVersionStatus = "draft"
	RiskProfileVersionStatusActive     RiskProfileVersionStatus = "active"
	RiskProfileVersionStatusSuperseded RiskProfileVersionStatus = "superseded"
	RiskProfileVersionStatusArchived   RiskProfileVersionStatus = "archived"
)

// RuleStatus is the lifecycle status of profile-version rules and gate policies.
type RuleStatus string

const (
	RuleStatusActive   RuleStatus = "active"
	RuleStatusDisabled RuleStatus = "disabled"
)

// RiskRuleKind classifies a policy matcher.
type RiskRuleKind string

const (
	RiskRuleKindPath          RiskRuleKind = "path"
	RiskRuleKindService       RiskRuleKind = "service"
	RiskRuleKindAPI           RiskRuleKind = "api"
	RiskRuleKindDatabase      RiskRuleKind = "database"
	RiskRuleKindSecret        RiskRuleKind = "secret"
	RiskRuleKindAuth          RiskRuleKind = "auth"
	RiskRuleKindRuntimeAction RiskRuleKind = "runtime_action"
	RiskRuleKindRelease       RiskRuleKind = "release"
	RiskRuleKindAutomation    RiskRuleKind = "automation"
	RiskRuleKindDocument      RiskRuleKind = "document"
	RiskRuleKindCustom        RiskRuleKind = "custom"
)

// GateKind classifies a required gate.
type GateKind string

const (
	GateKindProduct      GateKind = "product"
	GateKindArchitecture GateKind = "architecture"
	GateKindTechnical    GateKind = "technical"
	GateKindQA           GateKind = "qa"
	GateKindRelease      GateKind = "release"
	GateKindPostdeploy   GateKind = "postdeploy"
	GateKindEmergency    GateKind = "emergency"
	GateKindCustom       GateKind = "custom"
)

// RiskAssessmentStatus is the lifecycle status of a risk assessment.
type RiskAssessmentStatus string

const (
	RiskAssessmentStatusDraft      RiskAssessmentStatus = "draft"
	RiskAssessmentStatusActive     RiskAssessmentStatus = "active"
	RiskAssessmentStatusSuperseded RiskAssessmentStatus = "superseded"
	RiskAssessmentStatusClosed     RiskAssessmentStatus = "closed"
)

// RiskFactorSourceType classifies the source of a risk factor.
type RiskFactorSourceType string

const (
	RiskFactorSourceTypePolicy        RiskFactorSourceType = "policy"
	RiskFactorSourceTypeChangedFile   RiskFactorSourceType = "changed_file"
	RiskFactorSourceTypeService       RiskFactorSourceType = "service"
	RiskFactorSourceTypeAPI           RiskFactorSourceType = "api"
	RiskFactorSourceTypeDatabase      RiskFactorSourceType = "database"
	RiskFactorSourceTypeSecret        RiskFactorSourceType = "secret"
	RiskFactorSourceTypeRelease       RiskFactorSourceType = "release"
	RiskFactorSourceTypeRuntime       RiskFactorSourceType = "runtime"
	RiskFactorSourceTypeReviewSignal  RiskFactorSourceType = "review_signal"
	RiskFactorSourceTypeHumanDecision RiskFactorSourceType = "human_decision"
)

// ReviewRoleKind classifies the role that produced a review signal.
type ReviewRoleKind string

const (
	ReviewRoleKindReviewer          ReviewRoleKind = "reviewer"
	ReviewRoleKindQA                ReviewRoleKind = "qa"
	ReviewRoleKindLexicalGatekeeper ReviewRoleKind = "lexical_gatekeeper"
	ReviewRoleKindRiskGatekeeper    ReviewRoleKind = "risk_gatekeeper"
	ReviewRoleKindSRE               ReviewRoleKind = "sre"
	ReviewRoleKindSecurity          ReviewRoleKind = "security"
	ReviewRoleKindOwner             ReviewRoleKind = "owner"
	ReviewRoleKindCustom            ReviewRoleKind = "custom"
)

// ReviewSignalOutcome classifies the result of a review signal.
type ReviewSignalOutcome string

const (
	ReviewSignalOutcomePass           ReviewSignalOutcome = "pass"
	ReviewSignalOutcomePassWithNotes  ReviewSignalOutcome = "pass_with_notes"
	ReviewSignalOutcomeBlock          ReviewSignalOutcome = "block"
	ReviewSignalOutcomeRequestChanges ReviewSignalOutcome = "request_changes"
	ReviewSignalOutcomeRaiseRisk      ReviewSignalOutcome = "raise_risk"
	ReviewSignalOutcomeInformational  ReviewSignalOutcome = "informational"
)

// SignalSeverity classifies review and blocking signal severity.
type SignalSeverity string

const (
	SignalSeverityInfo     SignalSeverity = "info"
	SignalSeverityWarning  SignalSeverity = "warning"
	SignalSeverityBlocking SignalSeverity = "blocking"
	SignalSeverityCritical SignalSeverity = "critical"
)

// Confidence classifies confidence for automated or agent-produced signals.
type Confidence string

const (
	ConfidenceLow    Confidence = "low"
	ConfidenceMedium Confidence = "medium"
	ConfidenceHigh   Confidence = "high"
)

// GateRequestStatus is the lifecycle status of a gate request.
type GateRequestStatus string

const (
	GateRequestStatusRequested        GateRequestStatus = "requested"
	GateRequestStatusDelivering       GateRequestStatus = "delivering"
	GateRequestStatusAwaitingDecision GateRequestStatus = "awaiting_decision"
	GateRequestStatusResolved         GateRequestStatus = "resolved"
	GateRequestStatusExpired          GateRequestStatus = "expired"
	GateRequestStatusCancelled        GateRequestStatus = "cancelled"
)

// GateOutcome is the final outcome of a gate decision.
type GateOutcome string

const (
	GateOutcomeApprove               GateOutcome = "approve"
	GateOutcomeApproveWithConditions GateOutcome = "approve_with_conditions"
	GateOutcomeRevise                GateOutcome = "revise"
	GateOutcomeReject                GateOutcome = "reject"
	GateOutcomeHold                  GateOutcome = "hold"
	GateOutcomeRollback              GateOutcome = "rollback"
	GateOutcomeEscalate              GateOutcome = "escalate"
)

// SelfDeployPlanGateStatus is the summarized gate readiness for a self-deploy plan.
type SelfDeployPlanGateStatus string

const (
	SelfDeployPlanGateStatusPending        SelfDeployPlanGateStatus = "pending"
	SelfDeployPlanGateStatusApproved       SelfDeployPlanGateStatus = "approved"
	SelfDeployPlanGateStatusRejected       SelfDeployPlanGateStatus = "rejected"
	SelfDeployPlanGateStatusBlocked        SelfDeployPlanGateStatus = "blocked"
	SelfDeployPlanGateStatusRequestChanges SelfDeployPlanGateStatus = "request_changes"
)

// ReleaseDecisionPackageStatus is the lifecycle status of a release package.
type ReleaseDecisionPackageStatus string

const (
	ReleaseDecisionPackageStatusDraft             ReleaseDecisionPackageStatus = "draft"
	ReleaseDecisionPackageStatusReady             ReleaseDecisionPackageStatus = "ready"
	ReleaseDecisionPackageStatusDecisionRequested ReleaseDecisionPackageStatus = "decision_requested"
	ReleaseDecisionPackageStatusClosed            ReleaseDecisionPackageStatus = "closed"
)

// ReleaseDecisionStatus is the lifecycle status of a release decision.
type ReleaseDecisionStatus string

const (
	ReleaseDecisionStatusRequested ReleaseDecisionStatus = "requested"
	ReleaseDecisionStatusResolved  ReleaseDecisionStatus = "resolved"
	ReleaseDecisionStatusCancelled ReleaseDecisionStatus = "cancelled"
)

// ReleaseDecisionOutcome is the deterministic outcome of a release decision.
type ReleaseDecisionOutcome string

const (
	ReleaseDecisionOutcomeGo               ReleaseDecisionOutcome = "go"
	ReleaseDecisionOutcomeGoWithConditions ReleaseDecisionOutcome = "go_with_conditions"
	ReleaseDecisionOutcomeNoGo             ReleaseDecisionOutcome = "no_go"
	ReleaseDecisionOutcomeHold             ReleaseDecisionOutcome = "hold"
	ReleaseDecisionOutcomeRollback         ReleaseDecisionOutcome = "rollback"
	ReleaseDecisionOutcomeFollowUpRequired ReleaseDecisionOutcome = "follow_up_required"
)

// ReleaseSafetyStateKind classifies release safety-loop state.
type ReleaseSafetyStateKind string

const (
	ReleaseSafetyStateKindReleaseCandidate      ReleaseSafetyStateKind = "release_candidate"
	ReleaseSafetyStateKindAwaitingReleaseGate   ReleaseSafetyStateKind = "awaiting_release_gate"
	ReleaseSafetyStateKindDeploying             ReleaseSafetyStateKind = "deploying"
	ReleaseSafetyStateKindPostdeployObservation ReleaseSafetyStateKind = "postdeploy_observation"
	ReleaseSafetyStateKindStable                ReleaseSafetyStateKind = "stable"
	ReleaseSafetyStateKindHold                  ReleaseSafetyStateKind = "hold"
	ReleaseSafetyStateKindRollback              ReleaseSafetyStateKind = "rollback"
	ReleaseSafetyStateKindFollowUpRequired      ReleaseSafetyStateKind = "follow_up_required"
)

// BlockingSignalSourceType classifies the source of a release blocking signal.
type BlockingSignalSourceType string

const (
	BlockingSignalSourceTypeAcceptance     BlockingSignalSourceType = "acceptance"
	BlockingSignalSourceTypeReviewSignal   BlockingSignalSourceType = "review_signal"
	BlockingSignalSourceTypeRuntime        BlockingSignalSourceType = "runtime"
	BlockingSignalSourceTypeProvider       BlockingSignalSourceType = "provider"
	BlockingSignalSourceTypeInteraction    BlockingSignalSourceType = "interaction"
	BlockingSignalSourceTypeHuman          BlockingSignalSourceType = "human"
	BlockingSignalSourceTypeMonitoring     BlockingSignalSourceType = "monitoring"
	BlockingSignalSourceTypeSecurity       BlockingSignalSourceType = "security"
	BlockingSignalSourceTypeDependency     BlockingSignalSourceType = "dependency"
	BlockingSignalSourceTypeContainer      BlockingSignalSourceType = "container"
	BlockingSignalSourceTypeInfrastructure BlockingSignalSourceType = "infrastructure"
)

// BlockingSignalStatus is the lifecycle status of a blocking signal.
type BlockingSignalStatus string

const (
	BlockingSignalStatusActive    BlockingSignalStatus = "active"
	BlockingSignalStatusResolved  BlockingSignalStatus = "resolved"
	BlockingSignalStatusDismissed BlockingSignalStatus = "dismissed"
)

// GovernanceDecisionSummaryKind classifies a safe owner/staff read-model item.
type GovernanceDecisionSummaryKind string

const (
	GovernanceDecisionSummaryKindRiskAssessment         GovernanceDecisionSummaryKind = "risk_assessment"
	GovernanceDecisionSummaryKindReviewSignal           GovernanceDecisionSummaryKind = "review_signal"
	GovernanceDecisionSummaryKindGateRequest            GovernanceDecisionSummaryKind = "gate_request"
	GovernanceDecisionSummaryKindGateDecision           GovernanceDecisionSummaryKind = "gate_decision"
	GovernanceDecisionSummaryKindReleaseDecisionPackage GovernanceDecisionSummaryKind = "release_decision_package"
	GovernanceDecisionSummaryKindReleaseDecision        GovernanceDecisionSummaryKind = "release_decision"
	GovernanceDecisionSummaryKindBlockingSignal         GovernanceDecisionSummaryKind = "blocking_signal"
	GovernanceDecisionSummaryKindReleaseSafetyState     GovernanceDecisionSummaryKind = "release_safety_state"
)

// GovernanceDecisionAttention classifies whether a summary item needs owner action.
type GovernanceDecisionAttention string

const (
	GovernanceDecisionAttentionPending       GovernanceDecisionAttention = "pending"
	GovernanceDecisionAttentionCompleted     GovernanceDecisionAttention = "completed"
	GovernanceDecisionAttentionBlocked       GovernanceDecisionAttention = "blocked"
	GovernanceDecisionAttentionInformational GovernanceDecisionAttention = "informational"
)

// DecisionAuditKind classifies audit refs for gate and release decisions.
type DecisionAuditKind string

const (
	DecisionAuditKindGate    DecisionAuditKind = "gate"
	DecisionAuditKindRelease DecisionAuditKind = "release"
)
