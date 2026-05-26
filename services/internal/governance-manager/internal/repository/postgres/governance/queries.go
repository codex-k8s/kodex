package governance

import "fmt"

var (
	queryCommandResultCreate          = mustLoadQuery("command_result__create")
	queryCommandResultGet             = mustLoadQuery("command_result__get")
	queryGateDecisionCreate           = mustLoadQuery("gate_decision__create")
	queryGateDecisionGet              = mustLoadQuery("gate_decision__get")
	queryGateDecisionList             = mustLoadQuery("gate_decision__list")
	queryGatePolicyCreate             = mustLoadQuery("gate_policy__create")
	queryGatePolicyList               = mustLoadQuery("gate_policy__list")
	queryGateRequestCreate            = mustLoadQuery("gate_request__create")
	queryGateRequestGet               = mustLoadQuery("gate_request__get")
	queryGateRequestList              = mustLoadQuery("gate_request__list")
	queryGateRequestUpdate            = mustLoadQuery("gate_request__update")
	queryOutboxEventClaim             = mustLoadQuery("outbox_event__claim")
	queryOutboxEventCreate            = mustLoadQuery("outbox_event__create")
	queryOutboxEventMarkFailed        = mustLoadQuery("outbox_event__mark_failed")
	queryOutboxEventMarkPermanent     = mustLoadQuery("outbox_event__mark_permanently_failed")
	queryOutboxEventMarkPublished     = mustLoadQuery("outbox_event__mark_published")
	queryReleaseDecisionPackageCreate = mustLoadQuery("release_decision_package__create")
	queryReleaseDecisionPackageGet    = mustLoadQuery("release_decision_package__get")
	queryReleaseDecisionPackageList   = mustLoadQuery("release_decision_package__list")
	queryReviewSignalCreate           = mustLoadQuery("review_signal__create")
	queryReviewSignalGet              = mustLoadQuery("review_signal__get")
	queryReviewSignalList             = mustLoadQuery("review_signal__list")
	queryRiskAssessmentCreate         = mustLoadQuery("risk_assessment__create")
	queryRiskAssessmentGet            = mustLoadQuery("risk_assessment__get")
	queryRiskAssessmentList           = mustLoadQuery("risk_assessment__list")
	queryRiskAssessmentUpdate         = mustLoadQuery("risk_assessment__update")
	queryRiskFactorCreate             = mustLoadQuery("risk_factor__create")
	queryRiskFactorDeleteByAssessment = mustLoadQuery("risk_factor__delete_by_assessment")
	queryRiskFactorList               = mustLoadQuery("risk_factor__list")
	queryRiskProfileCreate            = mustLoadQuery("risk_profile__create")
	queryRiskProfileGet               = mustLoadQuery("risk_profile__get")
	queryRiskProfileList              = mustLoadQuery("risk_profile__list")
	queryRiskProfileUpdate            = mustLoadQuery("risk_profile__update")
	queryRiskProfileVersionActivate   = mustLoadQuery("risk_profile_version__activate")
	queryRiskProfileVersionCreate     = mustLoadQuery("risk_profile_version__create")
	queryRiskProfileVersionGet        = mustLoadQuery("risk_profile_version__get")
	queryRiskProfileVersionSupersede  = mustLoadQuery("risk_profile_version__supersede")
	queryRiskRuleCreate               = mustLoadQuery("risk_rule__create")
	queryRiskRuleList                 = mustLoadQuery("risk_rule__list")
)

func mustLoadQuery(name string) string {
	query, err := loadQuery(name)
	if err != nil {
		panic(err)
	}
	return query
}

func loadQuery(name string) (string, error) {
	data, err := SQLFiles.ReadFile("sql/" + name + ".sql")
	if err != nil {
		return "", fmt.Errorf("load sql query %s: %w", name, err)
	}
	return string(data), nil
}
