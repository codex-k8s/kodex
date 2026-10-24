package httptransport

import (
	"regexp"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// providerUsageProfileKey defines the bounded lowercase runtime-profile key grammar used only for
// CONFIGURE usage context. It does not resolve a profile or authorize its selection.
// providerUsageProfileKey задаёт ограниченную грамматику ключа runtime profile нижнего регистра
// только для контекста использования CONFIGURE. Профиль не разрешается и его выбор не
// авторизуется.
var providerUsageProfileKey = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,119}$`)

// providerUsageModel bounds the ASCII model locator accepted in configuration usage context,
// including provider separators; matching does not prove catalog availability.
// providerUsageModel ограничивает ASCII locator модели в контексте использования конфигурации,
// включая разделители провайдера; совпадение не доказывает доступность в каталоге.
var providerUsageModel = regexp.MustCompile(`^[a-zA-Z0-9._:/-]{1,128}$`)

// validProviderUsage checks version/count bounds, pinned catalog/context digests, six known
// dimensions and selection/submit consistency. Ready provider health requires a matching current
// remote catalog observation; absent context remains NOT_EVALUATED and cannot authorize submit.
// validProviderUsage проверяет границы версий и счётчиков, pins дайджестов каталога и контекста,
// шесть известных измерений и согласованность selection/submit. Готовое здоровье провайдера
// требует соответствующего актуального remote наблюдения; отсутствие контекста остаётся
// NOT_EVALUATED и не разрешает submit.
func validProviderUsage(u *cp.ProviderAccountUsage) bool {
	if u == nil || u.AccountVersion < 1 || u.AccountVersion > maximumSafeJSONInteger || u.AgentVersion < 0 || u.AgentVersion > maximumSafeJSONInteger ||
		u.MaximumConcurrentExecutions < 1 || u.MaximumConcurrentExecutions > maximumSafeJSONInteger || u.ActiveExecutions < 0 || u.ActiveExecutions > maximumSafeJSONInteger ||
		!modelCatalogDigest.MatchString(u.ContextDigest) || !modelCatalogDigest.MatchString(u.CatalogDigest) || u.CatalogRevision != "mcat_"+u.CatalogDigest ||
		u.ObservedAt == nil || u.ExpiresAt == nil || u.ObservedAt.CheckValid() != nil || u.ExpiresAt.CheckValid() != nil || !u.ExpiresAt.AsTime().After(u.ObservedAt.AsTime()) ||
		u.CatalogStatus == nil || u.ProviderHealthScope != cp.ProviderHealthScope_PROVIDER_HEALTH_SCOPE_CREDENTIALED_CATALOG_REACHABILITY {
		return false
	}
	ready := cp.ProviderUsageState_PROVIDER_USAGE_STATE_READY
	for _, d := range []*cp.ProviderUsageDimension{u.Lifecycle, u.Credential, u.ProviderHealth, u.ModelCompatibility, u.Capacity, u.ActorEligibility} {
		if d == nil || d.State == 0 || cp.ProviderUsageState_name[int32(d.State)] == "" || d.Reason == 0 || cp.ProviderUsageReason_name[int32(d.Reason)] == "" || d.Remediation == 0 || cp.ProviderUsageRemediation_name[int32(d.Remediation)] == "" {
			return false
		}
	}
	if u.OperationalState == 0 || cp.ProviderUsageState_name[int32(u.OperationalState)] == "" || (u.Capacity.State == ready) != (u.ActiveExecutions < u.MaximumConcurrentExecutions) ||
		u.EligibleForSelection && (u.Lifecycle.State != ready || u.Credential.State != ready || u.ActorEligibility.State != ready) || u.AllowedToSubmit && (!u.EligibleForSelection || u.ModelCompatibility.State != ready) {
		return false
	}
	if u.Context == nil {
		if u.AllowedToSubmit || u.EligibleForSelection || u.AgentVersion != 0 || u.ActorEligibility.State != cp.ProviderUsageState_PROVIDER_USAGE_STATE_NOT_EVALUATED || u.ModelCompatibility.State != cp.ProviderUsageState_PROVIDER_USAGE_STATE_NOT_EVALUATED {
			return false
		}
	} else if !validProviderUsageContext(u.Context) || u.AgentVersion < 1 {
		return false
	}
	if u.RuntimeConfigurationRef != "" && (!fileTargetRef(u.RuntimeConfigurationRef) || !modelCatalogDigest.MatchString(u.RuntimeConfigurationDigest)) || u.RuntimeConfigurationRef == "" && u.RuntimeConfigurationDigest != "" {
		return false
	}
	if u.ProviderHealth.State == ready {
		if u.ProviderHealth.Reason != cp.ProviderUsageReason_PROVIDER_USAGE_REASON_CREDENTIALED_CATALOG_REACHABLE || u.ProviderHealthObservedAt == nil || u.ProviderHealthExpiresAt == nil ||
			u.CatalogStatus.State != cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_READY || u.CatalogStatus.Failure != cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_NONE ||
			(u.CatalogStatus.Source != cp.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_REMOTE_API && u.CatalogStatus.Source != cp.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_REMOTE_CODEX) ||
			u.ProviderHealthObservedAt.CheckValid() != nil || u.ProviderHealthExpiresAt.CheckValid() != nil || u.ProviderHealthObservedAt.AsTime().After(u.ObservedAt.AsTime()) || !u.ProviderHealthExpiresAt.AsTime().After(u.ObservedAt.AsTime()) ||
			u.CatalogStatus.ObservedAt == nil || u.CatalogStatus.ExpiresAt == nil || !u.ProviderHealthObservedAt.AsTime().Equal(u.CatalogStatus.ObservedAt.AsTime()) || !u.ProviderHealthExpiresAt.AsTime().Equal(u.CatalogStatus.ExpiresAt.AsTime()) {
			return false
		}
	} else if u.ProviderHealth.State != cp.ProviderUsageState_PROVIDER_USAGE_STATE_UNKNOWN || u.ProviderHealth.Reason != cp.ProviderUsageReason_PROVIDER_USAGE_REASON_PROVIDER_HEALTH_UNOBSERVED || u.ProviderHealthObservedAt != nil || u.ProviderHealthExpiresAt != nil {
		return false
	}
	return true
}

// validProviderUsageContext requires an agent and distinguishes LAUNCH with no configuration
// overrides from CONFIGURE with a known-shaped provider/profile and optional model/effort pair.
// These are query coordinates, not caller authority.
// validProviderUsageContext требует агента и различает LAUNCH без переопределений конфигурации и
// CONFIGURE с корректной формой provider/profile и необязательной парой model/effort. Это
// координаты запроса, а не полномочия caller.
func validProviderUsageContext(c *cp.ProviderAccountUsageContext) bool {
	if c == nil || !fileTargetRef(c.AgentRef) {
		return false
	}
	if c.Purpose == cp.ProviderAccountUsagePurpose_PROVIDER_ACCOUNT_USAGE_PURPOSE_LAUNCH {
		return c.ProviderDefinitionKey == "" && c.RuntimeProfileRef == "" && c.Model == "" && c.ReasoningEffort == ""
	}
	return c.Purpose == cp.ProviderAccountUsagePurpose_PROVIDER_ACCOUNT_USAGE_PURPOSE_CONFIGURE && modelProviderKey.MatchString(c.ProviderDefinitionKey) && providerUsageProfileKey.MatchString(c.RuntimeProfileRef) &&
		(c.Model == "" || providerUsageModel.MatchString(c.Model)) && (c.ReasoningEffort == "" || c.Model != "" && runtimeEffortPattern.MatchString(c.ReasoningEffort))
}

// providerUsageContext returns nil/success for absent optional context; otherwise maps the purpose
// discriminator, builds the explicit context and validates it without applying defaults or looking
// up resources.
// providerUsageContext возвращает nil/успех при отсутствии необязательного контекста; иначе
// преобразует purpose, собирает явный контекст и проверяет его без значений по умолчанию и поиска
// ресурсов.
func providerUsageContext(present bool, purpose, agent, profile, provider, model, effort string) (*cp.ProviderAccountUsageContext, bool) {
	if !present {
		return nil, true
	}
	context := &cp.ProviderAccountUsageContext{Purpose: cp.ProviderAccountUsagePurpose(cp.ProviderAccountUsagePurpose_value["PROVIDER_ACCOUNT_USAGE_PURPOSE_"+purpose]), AgentRef: agent, RuntimeProfileRef: profile, ProviderDefinitionKey: provider, Model: model, ReasoningEffort: effort}
	return context, validProviderUsageContext(context)
}

// validProviderAccountRead binds a valid account usage projection to the same account version and
// protobuf-equal requested usage context, rejecting a response assembled for another context.
// validProviderAccountRead связывает корректную проекцию использования с той же версией аккаунта и
// protobuf-равным запрошенным контекстом, отклоняя ответ для другого контекста.
func validProviderAccountRead(account *cp.ProviderAccount, context *cp.ProviderAccountUsageContext) bool {
	return account != nil && validProviderUsage(account.Usage) && account.Version == account.Usage.AccountVersion && proto.Equal(account.Usage.Context, context)
}
