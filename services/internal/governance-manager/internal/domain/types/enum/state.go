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

// ReleaseDecisionPackageStatus is the lifecycle status of a release package.
type ReleaseDecisionPackageStatus string

const (
	ReleaseDecisionPackageStatusDraft             ReleaseDecisionPackageStatus = "draft"
	ReleaseDecisionPackageStatusReady             ReleaseDecisionPackageStatus = "ready"
	ReleaseDecisionPackageStatusDecisionRequested ReleaseDecisionPackageStatus = "decision_requested"
	ReleaseDecisionPackageStatusClosed            ReleaseDecisionPackageStatus = "closed"
)

// DecisionAuditKind classifies audit refs for gate and release decisions.
type DecisionAuditKind string

const (
	DecisionAuditKindGate    DecisionAuditKind = "gate"
	DecisionAuditKindRelease DecisionAuditKind = "release"
)
