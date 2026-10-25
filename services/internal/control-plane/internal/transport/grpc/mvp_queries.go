package grpc

import (
	"context"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/providerfinancewire"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	scheduleservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/schedule"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// ListProviderDefinitions returns the authorized provider-definition page after resolving the
// principal for this method.
// ListProviderDefinitions возвращает авторизованную страницу определений провайдеров после
// получения principal данного метода.
func (server *Server) ListProviderDefinitions(ctx context.Context, request *controlplanev1.ListProviderDefinitionsRequest) (*controlplanev1.ListProviderDefinitionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListProviderDefinitions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListProviderDefinitions(ctx, p, query.Filter{Query: request.GetQuery(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListProviderDefinitionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	response.Definitions = castResponseItems(items, castProviderDefinition)
	return response, nil
}

// ListProviderAccounts resolves the transport principal before choosing the
// financial-summary-only or paged account read. The summary branch rejects mixed
// filters; owner services determine scope/eligibility and wire casting does not
// turn observed balances into execution permission.
// ListProviderAccounts получает transport principal до выбора только финансового
// обзора либо списка аккаунтов. Обзор отвергает смешанные фильтры; owner services
// определяют scope/eligibility, а wire-проекция не превращает баланс в право запуска.
func (server *Server) ListProviderAccounts(ctx context.Context, request *controlplanev1.ListProviderAccountsRequest) (*controlplanev1.ListProviderAccountsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListProviderAccounts_FullMethodName)
	if err != nil {
		return nil, err
	}
	if request.GetFinancialSummaryOnly() {
		if request.GetPage() != nil || request.GetQuery() != "" || request.GetState() != controlplanev1.ProviderAccountState_PROVIDER_ACCOUNT_STATE_UNSPECIFIED || request.GetDefinitionKey() != "" || request.GetUsageContext() != nil {
			return nil, transportError(errs.ErrInvalid)
		}
		overview, err := server.service.GetProviderFinancialOverview(ctx, p)
		if err != nil {
			return nil, transportError(err)
		}
		result := &controlplanev1.ProviderFinancialOverview{Total: overview.Total, Exhausted: overview.Exhausted, Unknown: overview.Unknown, ObservedAt: timestamp(overview.ObservedAt), ValidUntil: timestamp(overview.ValidUntil)}
		for _, account := range overview.Accounts {
			projected := &controlplanev1.ProviderFinancialDisplayAccount{Ref: account.Ref, Name: account.Name}
			if summary := account.FinancialSummary; summary != nil {
				projected.FinancialSummary = &controlplanev1.ProviderFinancialSummary{Observation: providerfinancewire.ObservationToProto(summary.Observation), ReceivedAt: timestamp(summary.ReceivedAt), Version: summary.Version}
			}
			result.Accounts = append(result.Accounts, projected)
		}
		return &controlplanev1.ListProviderAccountsResponse{FinancialSummary: result}, nil
	}

	state := ""
	if request.GetState() != controlplanev1.ProviderAccountState_PROVIDER_ACCOUNT_STATE_UNSPECIFIED {
		state = enumSuffix(request.GetState(), "PROVIDER_ACCOUNT_STATE_")
	}
	items, next, actions, err := server.service.ListProviderAccounts(ctx, p, query.Filter{
		Query: request.GetQuery(), State: state, DefinitionKey: request.GetDefinitionKey(), Page: page(request.GetPage()), ProviderUsage: providerUsageContextInput(request.GetUsageContext()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListProviderAccountsResponse{
		Page: &controlplanev1.PageInfo{NextPageToken: next}, NextActions: nextActions(actions),
	}
	for _, item := range items {
		account, err := castProviderAccountUsageRead(item)
		if err != nil {
			return nil, transportError(err)
		}
		response.Accounts = append(response.Accounts, account)
	}
	return response, nil
}

// GetProviderAccount reads the account together with usage context and rejects errors from the
// typed usage projection.
// GetProviderAccount читает аккаунт вместе с контекстом использования и отклоняет ошибки
// типизированного преобразования использования.
func (server *Server) GetProviderAccount(ctx context.Context, request *controlplanev1.GetProviderAccountRequest) (*controlplanev1.GetProviderAccountResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetProviderAccount_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetProviderAccountWithUsage(ctx, p, request.GetAccountRef(), providerUsageContextInput(request.GetUsageContext()))
	if err != nil {
		return nil, transportError(err)
	}
	account, err := castProviderAccountUsageRead(item)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetProviderAccountResponse{Account: account}, nil
}

// castProviderDefinition projects provider availability/readiness and ordered authorization-method/model catalogs through distinct typed casters, without probing the provider or selecting an eligible account.
// castProviderDefinition отображает доступность и готовность провайдера и упорядоченные каталоги способов авторизации и моделей через разные типизированные преобразователи без проверки провайдера или выбора подходящего аккаунта.
func castProviderDefinition(value entity.ProviderDefinition) *controlplanev1.ProviderDefinition {
	result := &controlplanev1.ProviderDefinition{
		Key: value.Key, Name: value.Name, Description: value.Description, ModelIds: value.ModelIDs,
		DefaultModelId: value.DefaultModelID, Available: value.Available, Ready: value.Ready,
		ReadinessBlockers: value.ReadinessBlockers,
	}
	for _, method := range value.AuthorizationMethods {
		result.AuthorizationMethods = append(result.AuthorizationMethods, providerAuthorizationMethod(method))
	}
	for _, model := range value.Models {
		result.Models = append(result.Models, castModelCapability(model))
	}
	return result
}

// ListModelCapabilities passes provider, account, expected revision and digest to the domain
// model-catalog read.
// ListModelCapabilities передаёт провайдера, аккаунт, ожидаемую ревизию и digest доменному чтению
// каталога моделей.
func (server *Server) ListModelCapabilities(ctx context.Context, request *controlplanev1.ListModelCapabilitiesRequest) (*controlplanev1.ListModelCapabilitiesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListModelCapabilities_FullMethodName)
	if err != nil {
		return nil, err
	}
	catalog, err := server.service.ListModelCatalog(ctx, p, request.GetProviderDefinitionKey(), request.GetProviderAccountRef(), query.Filter{
		ExpectedCatalogRevision: request.GetExpectedCatalogRevision(), ExpectedCatalogDigest: request.GetExpectedCatalogDigest(),
		Query: request.GetQuery(), Page: page(request.GetPage()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	return castModelCatalog(catalog), nil
}

// castModelCatalog projects models, pagination and catalog revision pins while preserving optional
// status presence.
// castModelCatalog преобразует модели, пагинацию и привязки ревизии каталога, сохраняя наличие
// необязательного статуса.
func castModelCatalog(catalog entity.ModelCatalog) *controlplanev1.ListModelCapabilitiesResponse {
	response := &controlplanev1.ListModelCapabilitiesResponse{Total: catalog.Total, Page: &controlplanev1.PageInfo{NextPageToken: catalog.NextPageToken}, CatalogRevision: catalog.Revision, CatalogDigest: catalog.Digest}
	if catalog.Status != nil {
		value := catalog.Status
		response.CatalogStatus = &controlplanev1.ProviderModelCatalogStatus{
			State:      controlplanev1.ProviderModelCatalogState(controlplanev1.ProviderModelCatalogState_value["PROVIDER_MODEL_CATALOG_STATE_"+value.State]),
			Source:     controlplanev1.ProviderModelCatalogSource(controlplanev1.ProviderModelCatalogSource_value["PROVIDER_MODEL_CATALOG_SOURCE_"+value.Source]),
			Failure:    controlplanev1.ProviderModelCatalogFailure(controlplanev1.ProviderModelCatalogFailure_value["PROVIDER_MODEL_CATALOG_FAILURE_"+value.Failure]),
			ObservedAt: optionalTimestamp(value.ObservedAt), ExpiresAt: optionalTimestamp(value.ExpiresAt),
		}
	}
	for _, item := range catalog.Models {
		response.Models = append(response.Models, castModelCapability(item))
	}
	return response
}

// castModelCapability copies model reasoning options, availability, eligible account references
// and readiness blockers.
// castModelCapability копирует варианты рассуждения модели, доступность, ссылки подходящих
// аккаунтов и препятствия готовности.
func castModelCapability(value entity.ModelCapability) *controlplanev1.ModelCapability {
	return &controlplanev1.ModelCapability{Id: value.ID, ProviderDefinitionKey: value.ProviderDefinitionKey,
		ReasoningEfforts: value.ReasoningEfforts, DefaultReasoningEffort: value.DefaultReasoningEffort,
		Available: value.Available, EligibleProviderAccountRefs: value.EligibleProviderAccountRefs,
		ReadinessBlockers: value.ReadinessBlockers}
}

// castProviderAccount projects the supplied domain account, optional financial
// observation and authorization details without recomputing readiness or financial
// eligibility; the caller must supply an already authorized read projection.
// castProviderAccount переводит переданный доменный аккаунт, необязательные
// финансовое наблюдение и authorization details без пересчёта readiness
// и financial eligibility; caller передаёт уже авторизованную read-проекцию.
func castProviderAccount(value entity.ProviderAccount) *controlplanev1.ProviderAccount {
	result := &controlplanev1.ProviderAccount{
		Deletion: castProviderAccountDeletion(value.Deletion), Verification: castProviderAccountVerification(value.Verification),
		FinancialEligible: value.FinancialEligible, Ref: value.Ref, Version: value.Version, DefinitionKey: value.DefinitionKey, Name: value.Name,
		ExternalAccountMasked: value.ExternalAccountMasked, State: providerAccountState(value.State),
		Enabled: value.Enabled, Ready: value.Ready, MaximumConcurrentExecutions: value.MaximumConcurrentExecutions, NextActions: nextActions(value.NextActions),
		CreatedAt: timestamp(value.CreatedAt), UpdatedAt: timestamp(value.UpdatedAt), SafeStatusReason: value.SafeStatusReason,
	}
	if value.FinancialSummary != nil {
		result.FinancialSummary = &controlplanev1.ProviderFinancialSummary{Observation: providerfinancewire.ObservationToProto(value.FinancialSummary.Observation), ReceivedAt: timestamp(value.FinancialSummary.ReceivedAt), Version: value.FinancialSummary.Version}
	}
	if value.Authorization != nil {
		result.Authorization = &controlplanev1.ProviderAuthorization{
			Ref: value.Authorization.Ref, Method: providerAuthorizationMethod(value.Authorization.Method),
			State:           providerAuthorizationState(value.Authorization.State),
			VerificationUri: value.Authorization.VerificationURI, UserCode: value.Authorization.UserCode,
			ExpiresAt: optionalTimestamp(value.Authorization.ExpiresAt), SafeFailureCode: value.Authorization.SafeFailureCode,
		}
	}
	return result
}

// providerAuthorizationMethod looks up the prefixed authorization method enum; unknown strings
// retain the generated zero-value fallback.
// providerAuthorizationMethod ищет enum способа авторизации по префиксу; неизвестные строки
// сохраняют нулевое значение generated lookup.
func providerAuthorizationMethod(value string) controlplanev1.ProviderAuthorizationMethod {
	return controlplanev1.ProviderAuthorizationMethod(controlplanev1.ProviderAuthorizationMethod_value["PROVIDER_AUTHORIZATION_METHOD_"+value])
}

// providerAuthorizationState looks up the prefixed authorization state enum without independently
// validating domain state.
// providerAuthorizationState ищет enum состояния авторизации по префиксу без самостоятельной
// проверки доменного состояния.
func providerAuthorizationState(value string) controlplanev1.ProviderAuthorizationState {
	return controlplanev1.ProviderAuthorizationState(controlplanev1.ProviderAuthorizationState_value["PROVIDER_AUTHORIZATION_STATE_"+value])
}

// providerAccountState maps the prefixed domain account state through the generated enum table,
// including its zero fallback.
// providerAccountState переводит доменное состояние аккаунта через generated-таблицу enum с
// префиксом, включая нулевой fallback.
func providerAccountState(value string) controlplanev1.ProviderAccountState {
	return controlplanev1.ProviderAccountState(controlplanev1.ProviderAccountState_value["PROVIDER_ACCOUNT_STATE_"+value])
}

// GetRuntimeEnvironmentReadiness projects the domain environment readiness observation and
// published-version pins without probing the environment here.
// GetRuntimeEnvironmentReadiness преобразует доменное наблюдение готовности среды и привязки
// опубликованной версии без проверки среды в этом методе.
func (server *Server) GetRuntimeEnvironmentReadiness(ctx context.Context, request *controlplanev1.GetRuntimeEnvironmentReadinessRequest) (*controlplanev1.GetRuntimeEnvironmentReadinessResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_GetRuntimeEnvironmentReadiness_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetRuntimeEnvironmentReadiness(ctx, p, request.GetEnvironmentRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetRuntimeEnvironmentReadinessResponse{Readiness: &controlplanev1.RuntimeEnvironmentReadiness{
		EnvironmentRef: item.EnvironmentRef, EnvironmentVersion: item.EnvironmentVersion,
		PublishedVersionRef: item.PublishedVersionRef, PublishedVersionDigest: item.PublishedVersionDigest,
		Ready: item.Ready, Blockers: item.Blockers, ObservedAt: timestamp(item.ObservedAt),
	}}, nil
}

// ListRuntimeEnvironmentAgents reads the authorized environment-agent page using environment
// reference, query and page filters.
// ListRuntimeEnvironmentAgents читает авторизованную страницу агентов среды по ссылке среды,
// запросу и фильтрам страницы.
func (server *Server) ListRuntimeEnvironmentAgents(ctx context.Context, request *controlplanev1.ListRuntimeEnvironmentAgentsRequest) (*controlplanev1.ListRuntimeEnvironmentAgentsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListRuntimeEnvironmentAgents_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListRuntimeEnvironmentAgents(ctx, p, query.Filter{
		ResourceRef: request.GetEnvironmentRef(), Query: request.GetQuery(), Page: page(request.GetPage()),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRuntimeEnvironmentAgentsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	response.Agents = castResponseItems(items, castAgent)
	return response, nil
}

// ListScheduleRevisions returns schedule revisions and the continuation token from the authorized
// domain read.
// ListScheduleRevisions возвращает ревизии расписания и токен продолжения из авторизованного
// доменного чтения.
func (server *Server) ListScheduleRevisions(ctx context.Context, request *controlplanev1.ListScheduleRevisionsRequest) (*controlplanev1.ListScheduleRevisionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListScheduleRevisions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListScheduleRevisions(ctx, p, query.Filter{ResourceRef: request.GetScheduleRef(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListScheduleRevisionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	response.Revisions = castResponseItems(items, castScheduleRevision)
	return response, nil
}

// castScheduleRevision projects immutable schedule fields, target version and digest, prompt
// inputs and occurrence policies.
// castScheduleRevision преобразует неизменяемые поля расписания, версию и digest цели, входы
// промпта и политики срабатывания.
func castScheduleRevision(value entity.ScheduleRevision) *controlplanev1.ScheduleRevision {
	target := castRunTarget(value.Target)
	target.TargetVersion = value.TargetVersion
	return &controlplanev1.ScheduleRevision{
		Ref: value.Ref, Revision: value.Revision, Digest: value.Digest, Name: value.Name,
		Target: target, Preset: value.Preset, CronExpression: value.CronExpression,
		Timezone: value.Timezone, Input: structure(value.Input), SessionPolicy: value.SessionPolicy,
		NotificationPolicy: value.NotificationPolicy, CreatedAt: timestamp(value.CreatedAt),
		DstGapPolicy: value.DSTGapPolicy, DstFoldPolicy: value.DSTFoldPolicy,
		MisfirePolicy: value.MisfirePolicy, OverlapPolicy: value.OverlapPolicy,
		TargetVersion: value.TargetVersion, TargetDigest: value.TargetDigest,
		AutomationText: value.AutomationText, PromptInputs: structure(value.PromptInputs),
	}
}

// PreviewSchedule normalizes timing and previews occurrences, optionally projecting pinned prompt
// materialization with a validated preview mode.
// PreviewSchedule нормализует время и показывает срабатывания, при необходимости преобразуя
// материализацию промпта с привязками и проверенным режимом.
func (server *Server) PreviewSchedule(ctx context.Context, request *controlplanev1.PreviewScheduleRequest) (*controlplanev1.PreviewScheduleResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_PreviewSchedule_FullMethodName)
	if err != nil {
		return nil, err
	}
	var after time.Time
	if request.GetAfter() != nil {
		after = request.GetAfter().AsTime()
	}
	normalized, occurrences, err := server.service.PreviewSchedule(ctx, p, scheduleservice.Spec{
		Preset: request.GetPreset(), CronExpression: request.GetCronExpression(), TimeOfDay: request.GetTimeOfDay(),
		DayOfWeek: request.GetDayOfWeek(), Timezone: request.GetTimezone(), DSTGapPolicy: request.GetDstGapPolicy(),
		DSTFoldPolicy: request.GetDstFoldPolicy(), MisfirePolicy: request.GetMisfirePolicy(), OverlapPolicy: request.GetOverlapPolicy(),
	}, after, request.GetLimit())
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.PreviewScheduleResponse{
		NormalizedCronExpression: normalized.CronExpression, DstGapPolicy: normalized.DSTGapPolicy,
		DstFoldPolicy: normalized.DSTFoldPolicy, MisfirePolicy: normalized.MisfirePolicy,
		OverlapPolicy: normalized.OverlapPolicy,
	}
	for _, occurrence := range occurrences {
		response.Occurrences = append(response.Occurrences, timestamp(occurrence))
	}
	if materialization := request.GetMaterialization(); materialization != nil {
		if len(occurrences) == 0 {
			return nil, transportError(errs.ErrInvalid)
		}
		input := command.ScheduleInput{ProjectRef: materialization.GetProjectRef(), Name: materialization.GetName(), Target: runTarget(materialization.GetTarget()), TargetVersion: materialization.GetTarget().GetTargetVersion(),
			AutomationText: materialization.GetTask(), Input: materialization.GetInput().AsMap(), PromptInputs: materialization.GetPromptInputs().AsMap(),
			SessionPolicy: materialization.GetSessionPolicy(), NotificationPolicy: materialization.GetNotificationPolicy(),
			Preset: normalized.Preset, CronExpression: normalized.CronExpression, Timezone: normalized.Timezone, TimeOfDay: normalized.TimeOfDay, DayOfWeek: normalized.DayOfWeek,
			DSTGapPolicy: normalized.DSTGapPolicy, DSTFoldPolicy: normalized.DSTFoldPolicy, MisfirePolicy: normalized.MisfirePolicy, OverlapPolicy: normalized.OverlapPolicy}
		mode := "DRAFT"
		switch materialization.GetMode() {
		case controlplanev1.SchedulePromptPreviewMode_SCHEDULE_PROMPT_PREVIEW_MODE_UNSPECIFIED, controlplanev1.SchedulePromptPreviewMode_SCHEDULE_PROMPT_PREVIEW_MODE_DRAFT:
		case controlplanev1.SchedulePromptPreviewMode_SCHEDULE_PROMPT_PREVIEW_MODE_CURRENT_REVISION:
			mode = "CURRENT_REVISION"
		default:
			return nil, transportError(errs.ErrInvalid)
		}
		result, pin, variables, previewErr := server.service.PreviewScheduleMaterialization(ctx, p, input, materialization.GetScheduleRef(), materialization.GetExpectedScheduleVersion(), occurrences[0], materialization.GetExpectedContextDigest(), materialization.GetIncludeFullMaterialization(), mode)
		if previewErr != nil {
			return nil, transportError(previewErr)
		}
		response.MaterializedPrompt, err = castPromptMaterialization(result, materialization.GetIncludeFullMaterialization())
		if err != nil {
			return nil, err
		}
		response.MaterializationPin = &controlplanev1.SchedulePromptPreviewPin{ScheduleRef: pin.ScheduleRef, ScheduleVersion: pin.ScheduleVersion, RevisionRef: pin.RevisionRef, RevisionDigest: pin.RevisionDigest, ScheduledFor: timestamp(pin.ScheduledFor), Timezone: pin.Timezone, SessionRef: pin.SessionRef, Continuation: pin.Continuation, RevisionAvailable: pin.RevisionAvailable}
		response.MaterializationPin.Mode = controlplanev1.SchedulePromptPreviewMode(controlplanev1.SchedulePromptPreviewMode_value["SCHEDULE_PROMPT_PREVIEW_MODE_"+pin.Mode])
		response.MaterializationPin.ExecutionActorRef = pin.ExecutionActorRef
		response.MaterializationPin.BaseRevisionRef, response.MaterializationPin.BaseRevisionDigest = pin.BaseRevisionRef, pin.BaseRevisionDigest
		for _, item := range variables {
			reason, reasonErr := castTemplateAvailabilityReason(item.Reason, item.Available)
			if reasonErr != nil {
				return nil, transportError(reasonErr)
			}
			response.AutomationVariables = append(response.AutomationVariables, &controlplanev1.TemplateVariable{Name: item.Name, ValueType: item.Type, Description: item.Description, Example: item.Example, Source: item.Source, Available: item.Available, Reason: reason})
		}
	}
	return response, nil
}

// ListScheduleRuns returns authorized schedule occurrences with their schedule revision and run
// projections.
// ListScheduleRuns возвращает авторизованные срабатывания расписания с ревизией расписания и
// представлением запуска.
func (server *Server) ListScheduleRuns(ctx context.Context, request *controlplanev1.ListScheduleRunsRequest) (*controlplanev1.ListScheduleRunsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListScheduleRuns_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListScheduleRuns(ctx, p, query.Filter{ResourceRef: request.GetScheduleRef(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListScheduleRunsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Occurrences = append(response.Occurrences, &controlplanev1.ScheduleRunOccurrence{
			ScheduleRef: item.ScheduleRef, ScheduleRevisionRef: item.ScheduleRevisionRef,
			ScheduleRevision: item.ScheduleRevision, Run: castRun(item.Run),
		})
	}
	return response, nil
}

// ListRoleImageRecipeRevisions projects the authorized recipe-revision page including build,
// provenance, image and promotion pins.
// ListRoleImageRecipeRevisions преобразует авторизованную страницу ревизий рецепта с привязками
// сборки, происхождения, образа и продвижения.
func (server *Server) ListRoleImageRecipeRevisions(ctx context.Context, request *controlplanev1.ListRoleImageRecipeRevisionsRequest) (*controlplanev1.ListRoleImageRecipeRevisionsResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListRoleImageRecipeRevisions_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListRoleImageRecipeRevisions(ctx, p, query.Filter{ResourceRef: request.GetRecipeRef(), Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListRoleImageRecipeRevisionsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Revisions = append(response.Revisions, &controlplanev1.RoleImageRecipeRevision{
			Ref: item.Ref, RecipeRef: item.RecipeRef, Revision: int64(item.Revision), RecipeVersion: int64(item.RecipeVersion),
			RecipeGeneration: int64(item.RecipeGeneration), SpecSha256: item.SpecSHA256,
			ProvenanceSha256: item.ProvenanceSHA256, SourceSha256: item.SourceSHA256,
			ImmutableBuildSha256: item.ImmutableBuildSHA256, ImageArtifactRef: item.ImageArtifactRef,
			ManifestDigest: item.ManifestDigest, PromotedReference: item.PromotedReference,
			PromotionReceiptSha256: item.PromotionReceiptSHA256, CreatedAt: timestamp(item.CreatedAt),
		})
	}
	return response, nil
}

// normalizedProviderState removes the provider-account-state prefix from the enum string without
// additional state validation.
// normalizedProviderState удаляет префикс состояния аккаунта провайдера из строки enum без
// дополнительной проверки состояния.
func normalizedProviderState(value controlplanev1.ProviderAccountState) string {
	return strings.TrimPrefix(value.String(), "PROVIDER_ACCOUNT_STATE_")
}
