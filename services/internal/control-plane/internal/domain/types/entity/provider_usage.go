package entity

import "time"

// ProviderAccountUsageContext supplies the purpose, agent, provider definition, model, reasoning
// effort, and runtime profile used for an account-usage evaluation. These selectors are resolved
// within the caller's scope rather than conferring account access.
// ProviderAccountUsageContext передаёт назначение, сотрудника, определение провайдера, модель, степень
// рассуждения и профиль исполнения для оценки использования аккаунта. Признаки выбора разрешаются в
// области вызывающего субъекта и не предоставляют доступ к аккаунту.
type ProviderAccountUsageContext struct {
	Purpose, AgentRef, ProviderDefinitionKey, Model, ReasoningEffort, RuntimeProfileRef string
}

// ProviderUsageDimension explains one independently evaluated usage dimension through state, reason,
// and remediation. It is descriptive output and does not by itself determine the combined submission
// decision.
// ProviderUsageDimension объясняет отдельное измерение использования через состояние, причину и способ
// устранения. Это описательный результат, сам не определяющий общее решение о допустимости задачи.
type ProviderUsageDimension struct {
	State, Reason, Remediation string
}

// ProviderAccountUsage combines lifecycle, credential, provider-health, model, capacity, and actor
// evaluations with exact account, agent, runtime configuration, and catalogue pins and freshness
// times. Submission and selection decisions are distinct, and maximum and active execution counts do
// not replace the owner's atomic claim.
// ProviderAccountUsage объединяет оценки жизненного цикла, реквизитов, состояния провайдера, модели,
// ёмкости и субъекта с точными привязками аккаунта, сотрудника, конфигурации исполнения и каталога и
// временем свежести. Решения об отправке и выборе различаются; максимум и текущее число исполнений не
// заменяют атомарный захват владельца.
type ProviderAccountUsage struct {
	Context                                                                               *ProviderAccountUsageContext
	AccountVersion, AgentVersion                                                          int64
	RuntimeConfigurationRef, RuntimeConfigurationDigest                                   string
	Lifecycle, Credential, ProviderHealth, ModelCompatibility, Capacity, ActorEligibility ProviderUsageDimension
	AllowedToSubmit, EligibleForSelection                                                 bool
	OperationalState                                                                      string
	MaximumConcurrentExecutions, ActiveExecutions                                         int64
	CatalogStatus                                                                         *ModelCatalogStatus
	CatalogRevision, CatalogDigest, ContextDigest                                         string
	ObservedAt, ExpiresAt                                                                 time.Time
	ProviderHealthScope                                                                   string
	ProviderHealthObservedAt, ProviderHealthExpiresAt                                     *time.Time
}
