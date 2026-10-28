package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// assistantRuntimeEditable defines the closed user-editable runtime proposal fields, including
// model/profile/account choices and optional web-search mode; owner pins are supplied separately.
// assistantRuntimeEditable задаёт закрытые редактируемые поля runtime proposal, включая
// model/profile/account choices и необязательный web-search mode; owner pins назначаются отдельно.
var assistantRuntimeEditable = []string{"agentRef", "runtimeProfileRef", "model", "reasoningEffort", "webSearchMode", "providerPolicyMode", "providerAccounts"}

// assistantRuntimeOwnerFields defines assistant identity/scope/organization/project/profile pins
// that edits must preserve and hydration resolves from authoritative state.
// assistantRuntimeOwnerFields задаёт assistant identity/scope/organization/project/profile pins,
// которые правки должны сохранять и hydration разрешает по авторитетному состоянию.
var assistantRuntimeOwnerFields = []string{"agentRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef", "targetKind"}

// errAssistantRuntimeConfigurationNoChange marks only a fully validated unchanged proposal while
// retaining Conflict compatibility; ordinary conflict/readiness failures must not be interpreted
// as this marker.
// errAssistantRuntimeConfigurationNoChange помечает только полностью проверенное неизменённое
// предложение, сохраняя совместимость с Conflict; обычные conflict/readiness failures нельзя
// трактовать как этот маркер.
var errAssistantRuntimeConfigurationNoChange = errors.Join(errs.ErrConflict, errors.New("assistant runtime configuration is unchanged"))

// assistantRuntimeConfigurationUnchanged compares exact runtime/search/profile/account settings
// and nonempty persisted catalog pins. Copies are sorted by account after clearing only the two
// fields publication does not persist; provider identity, catalog revision/digest and caller
// slices remain intact.
// assistantRuntimeConfigurationUnchanged сравнивает точные runtime/search/profile/account settings
// и непустые сохранённые catalog pins. Копии сортируются по account после очистки лишь двух полей,
// которые publication не сохраняет; provider identity, catalog revision/digest и slices caller
// сохраняются.
func assistantRuntimeConfigurationUnchanged(before, after map[string]any, persisted []entity.ProviderAccountCandidate) bool {
	if assistantString(before, "webSearchMode") != assistantString(after, "webSearchMode") {
		return false
	}
	for _, field := range []string{"runtimeProfileRef", "model", "reasoningEffort", "providerPolicyMode"} {
		previous, previousOK := before[field].(string)
		prepared, preparedOK := after[field].(string)
		if !previousOK || !preparedOK || previous != prepared {
			return false
		}
	}
	if !assistantJSONEqual(before["runtimeProfilePin"], after["runtimeProfilePin"]) ||
		!assistantJSONEqual(before["providerAccounts"], after["providerAccounts"]) {
		return false
	}
	prepared, ok := after["providerCatalogPins"].([]entity.ProviderAccountCandidate)
	if !ok || len(persisted) == 0 || len(persisted) != len(prepared) {
		return false
	}
	// Эти два поля publication намеренно не сохраняет. Catalog revision/digest
	// и provider identity остаются обязательной частью точного сравнения.
	canonical := func(entries []entity.ProviderAccountCandidate) []entity.ProviderAccountCandidate {
		result := append([]entity.ProviderAccountCandidate(nil), entries...)
		for index := range result {
			result[index].DefaultReasoningEffort = ""
			result[index].ModelCapabilityDigest = ""
		}
		sort.Slice(result, func(i, j int) bool { return result[i].AccountRef < result[j].AccountRef })
		return result
	}
	return assistantJSONEqual(canonical(persisted), canonical(prepared))
}

// assistantRuntimeOverlay parses the existing restricted TOML overlay, replaces reasoning effort
// and changes search mode only when supplied, then encodes/canonicalizes it. Existing parse
// failure is Conflict; unsupported mode or encoding/canonicalization failure is Invalid,
// preserving unrelated supported settings.
// assistantRuntimeOverlay разбирает существующий ограниченный TOML overlay, заменяет reasoning
// effort и меняет search mode лишь при передаче, затем кодирует/канонизирует. Ошибка разбора
// исходника даёт Conflict; неподдерживаемый mode либо encoding/canonicalization failure даёт
// Invalid, сохраняя остальные поддерживаемые настройки.
func assistantRuntimeOverlay(current, effort, mode string) (string, error) {
	overlay, err := runtimecontract.ParseConfigOverlay(current)
	if err != nil {
		return "", errs.ErrConflict
	}
	overlay.ModelReasoningEffort = effort
	if mode != "" {
		if !runtimecontract.ValidWebSearchMode(mode) {
			return "", errs.ErrInvalid
		}
		overlay.WebSearchMode = mode
	}
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(overlay); err != nil {
		return "", errs.ErrInvalid
	}
	canonical, _, err := runtimecontract.CanonicalConfigOverlay(encoded.String())
	if err != nil {
		return "", errs.ErrInvalid
	}
	return canonical, nil
}

// assistantRuntimeAccounts decodes a required one-to-128 account array through JSON, permits only
// accountRef/weight and integer weights1..100, then sorts by accountRef. Account existence,
// uniqueness and catalog eligibility are validated later.
// assistantRuntimeAccounts декодирует обязательный массив от одного до128 accounts через JSON,
// разрешает только accountRef/weight и целые веса1..100, затем сортирует по accountRef.
// Существование account, уникальность и catalog eligibility проверяются позднее.
func assistantRuntimeAccounts(parameters map[string]any) ([]entity.ProviderAccountCandidate, error) {
	raw, exists := parameters["providerAccounts"]
	if !exists {
		return nil, errs.ErrInvalid
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, errs.ErrInvalid
	}
	var entries []map[string]any
	if json.Unmarshal(encoded, &entries) != nil || len(entries) < 1 || len(entries) > 128 {
		return nil, errs.ErrInvalid
	}
	result := make([]entity.ProviderAccountCandidate, 0, len(entries))
	for _, entry := range entries {
		if !onlyAssistantFields(entry, "accountRef", "weight") {
			return nil, errs.ErrInvalid
		}
		weight, ok := assistantInt64(entry, "weight")
		if !ok || weight < 1 || weight > 100 {
			return nil, errs.ErrInvalid
		}
		result = append(result, entity.ProviderAccountCandidate{AccountRef: assistantString(entry, "accountRef"), Weight: int32(weight)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].AccountRef < result[j].AccountRef })
	return result, nil
}

// minimalAssistantRuntimeAccounts copies only accountRef/weight into a new ordered map slice for
// editable parameters, excluding server-owned provider/catalog pins and returning an empty nonnil
// slice for no candidates.
// minimalAssistantRuntimeAccounts копирует только accountRef/weight в новый упорядоченный map
// slice для editable parameters, исключая серверные provider/catalog pins и возвращая пустой
// ненулевой slice при отсутствии candidates.
func minimalAssistantRuntimeAccounts(candidates []entity.ProviderAccountCandidate) []map[string]any {
	result := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, map[string]any{"accountRef": candidate.AccountRef, "weight": candidate.Weight})
	}
	return result
}

// assistantRuntimeOwner emits one server-owned target lineage: assistant scope/profile or ordinary AGENT/project, never a mixed policy.
// assistantRuntimeOwner возвращает одну назначенную сервером цепочку цели: scope/profile помощника либо AGENT/project обычного сотрудника, без смешения политик.
func assistantRuntimeOwner(target assistantConfigurationTarget, current scope, agentRef string) map[string]any {
	if target.ordinary {
		return map[string]any{"agentRef": agentRef, "targetKind": "AGENT", "scopeKind": "PROJECT", "organizationRef": current.organizationRef, "projectRef": target.projectRef}
	}
	assistantScope := "PROJECT"
	if target.scopeKind == "ORGANIZATION" {
		assistantScope = "SYSTEM"
	}
	return map[string]any{"agentRef": agentRef, "assistantScope": assistantScope, "scopeKind": target.scopeKind, "organizationRef": current.organizationRef, "projectRef": target.projectRef, "assistantProfileRef": target.profileRef}
}

// queryAssistantConfigurationProfilePin embeds stable-key profile version/runtime-revision/enabled
// lookup with FOR SHARE, keeping profile publication stable within the caller transaction.
// queryAssistantConfigurationProfilePin встраивает stable-key чтение profile
// version/runtime-revision/enabled с FOR SHARE, сохраняя profile publication стабильной внутри
// транзакции caller.
//
//go:embed sql/assistant_configuration__profile_pin.sql
var queryAssistantConfigurationProfilePin string

// assistantRuntimeProfilePin reads the locked profile pin, mapping any lookup failure to NotFound;
// requested enablement, positive version and nonempty runtime revision are required or Conflict is
// returned.
// assistantRuntimeProfilePin читает profile pin с блокировкой, отображая любую ошибку поиска в
// NotFound; требуемое enablement, положительная version и непустая runtime revision обязательны,
// иначе Conflict.
func assistantRuntimeProfilePin(ctx context.Context, tx pgx.Tx, profileRef string, requireEnabled bool) (entity.AssistantRuntimeProfilePin, error) {
	var pin entity.AssistantRuntimeProfilePin
	var enabled bool
	if err := tx.QueryRow(ctx, queryAssistantConfigurationProfilePin, pgx.StrictNamedArgs{"profile_ref": profileRef}).Scan(&pin.Ref, &pin.Version, &pin.RuntimeRevision, &enabled); err != nil {
		return pin, errs.ErrNotFound
	}
	if requireEnabled && !enabled || pin.Version < 1 || pin.RuntimeRevision == "" {
		return pin, errs.ErrConflict
	}
	return pin, nil
}

// assistantRuntimeBefore captures owner, agent/configuration/profile/overlay/catalog-choice
// metadata and optional draft pins from the current view. Published overlay parse success adds
// effort and nonempty search mode; parse failure leaves those optional fields absent without
// granting readiness.
// assistantRuntimeBefore сохраняет owner, agent/configuration/profile/overlay/catalog-choice
// metadata и optional draft pins текущего view. Успешный published overlay parse добавляет effort
// и непустой search mode; ошибка оставляет необязательные поля отсутствующими, не подтверждая
// readiness.
func assistantRuntimeBefore(target assistantConfigurationTarget, current scope, view entity.AgentRuntimeConfigurationView, pin entity.AssistantRuntimeProfilePin) map[string]any {
	before := assistantRuntimeOwner(target, current, view.Configuration.AgentRef)
	before["runtimeProfilePin"] = pin
	before["agentVersion"] = view.AgentVersion
	before["runtimeProfileRef"], before["model"], before["providerPolicyMode"] = view.Configuration.RuntimeProfileRef, view.Configuration.Model, view.Configuration.ProviderPolicy.Mode
	before["providerAccounts"] = minimalAssistantRuntimeAccounts(view.Configuration.ProviderPolicy.AccountCandidates)
	before["configurationRef"], before["configurationVersion"], before["configurationDigest"] = view.Configuration.Ref, view.Configuration.Version, view.Configuration.Digest
	before["publishedOverlayRef"], before["publishedOverlayVersion"], before["publishedOverlayDigest"] = view.PublishedOverlay.Ref, view.PublishedOverlay.Version, view.PublishedOverlay.Digest
	before["publishedOverlayContent"] = view.PublishedOverlay.Content
	before["draftOverlayRef"], before["draftOverlayVersion"], before["draftOverlayDigest"] = "", int64(0), ""
	if view.DraftOverlay != nil {
		before["draftOverlayRef"], before["draftOverlayVersion"], before["draftOverlayDigest"] = view.DraftOverlay.Ref, view.DraftOverlay.Version, view.DraftOverlay.Digest
	}
	if parsed, err := runtimecontract.ParseConfigOverlay(view.PublishedOverlay.Content); err == nil {
		before["reasoningEffort"] = parsed.ModelReasoningEffort
		if parsed.WebSearchMode != "" {
			before["webSearchMode"] = parsed.WebSearchMode
		}
	}
	return before
}

// prepareAssistantRuntimeCandidates validates account/model/policy shape, checks all selected
// accounts against the tenant runtime profile, loads exact provider catalog revisions/digests and
// validates normalized model/overlay capabilities. It returns current pinned candidates without
// opening a provider session.
// prepareAssistantRuntimeCandidates проверяет account/model/policy shape, сверяет все выбранные
// accounts с tenant runtime profile, загружает точные provider catalog revisions/digests и
// проверяет нормализованные model/overlay capabilities. Возвращает текущие закреплённые candidates
// без открытия provider session.
func (repository *Repository) prepareAssistantRuntimeCandidates(ctx context.Context, tx pgx.Tx, current scope, parameters map[string]any, overlay string) ([]entity.ProviderAccountCandidate, error) {
	candidates, err := assistantRuntimeAccounts(parameters)
	if err != nil || !validProviderPolicy(assistantString(parameters, "providerPolicyMode"), candidates) || !validModel(assistantString(parameters, "model")) {
		return nil, errs.ErrInvalid
	}
	refs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		refs = append(refs, candidate.AccountRef)
	}
	var provider, defaultModel, revision string
	var eligible int32
	err = tx.QueryRow(ctx, queryRuntimeConfigurationValidateAccounts, current.organizationID, assistantString(parameters, "runtimeProfileRef"), refs).Scan(&provider, &defaultModel, &revision, &eligible)
	if err != nil || eligible != int32(len(refs)) {
		return nil, errs.ErrConflict
	}
	for i := range candidates {
		catalog, err := readModelCatalogTx(ctx, tx, current, provider, candidates[i].AccountRef)
		if err != nil {
			return nil, err
		}
		candidates[i].ProviderDefinitionKey = provider
		candidates[i].CatalogRevision, candidates[i].CatalogDigest = catalog.Revision, catalog.Digest
	}
	normalized, _, err := validateRuntimeCatalogCandidates(ctx, tx, current, provider, assistantString(parameters, "model"), overlay, candidates, true)
	return normalized, err
}

// hydrateAssistantRuntimeConfiguration preserves the assistant-only entry point while sharing preparation with the ordinary-agent operation.
// hydrateAssistantRuntimeConfiguration сохраняет вход только для помощников и общий механизм подготовки с отдельной операцией сотрудника.
func (repository *Repository) hydrateAssistantRuntimeConfiguration(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	return repository.hydratePreparedRuntimeConfiguration(ctx, tx, current, "", operation)
}

// hydratePreparedRuntimeConfiguration captures exact configuration, overlay and provider pins after resolving the command-specific target policy.
// hydratePreparedRuntimeConfiguration фиксирует точные configuration, overlay и provider pins после проверки отдельной политики цели команды.
func (repository *Repository) hydratePreparedRuntimeConfiguration(ctx context.Context, tx pgx.Tx, current scope, projectRef string, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(operation.Parameters, assistantRuntimeEditable...) {
		return operation, errs.ErrInvalid
	}
	agentRef := assistantString(operation.Parameters, "agentRef")
	target, err := repository.preparedRuntimeTarget(ctx, tx, current, agentRef, projectRef, operation.Type == prepareAgentRuntimeConfiguration)
	if err != nil {
		return operation, err
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, current, agentRef)
	if err != nil {
		return operation, err
	}
	if view.DraftOverlay != nil {
		return operation, errs.ErrConflict
	}
	effort, ok := operation.Parameters["reasoningEffort"].(string)
	if !ok {
		return operation, errs.ErrInvalid
	}
	mode := ""
	if raw, present := operation.Parameters["webSearchMode"]; present {
		var valid bool
		mode, valid = raw.(string)
		if !valid || !runtimecontract.ValidWebSearchMode(mode) {
			return operation, errs.ErrInvalid
		}
	} else if parsed, parseErr := runtimecontract.ParseConfigOverlay(view.PublishedOverlay.Content); parseErr == nil {
		mode = parsed.WebSearchMode
	}
	overlay, err := assistantRuntimeOverlay(view.PublishedOverlay.Content, effort, mode)
	if err != nil {
		return operation, err
	}
	candidates, err := repository.prepareAssistantRuntimeCandidates(ctx, tx, current, operation.Parameters, overlay)
	if err != nil {
		return operation, err
	}
	after := assistantRuntimeOwner(target, current, agentRef)
	if mode != "" {
		after["webSearchMode"] = mode
	}
	for _, field := range []string{"runtimeProfileRef", "model", "reasoningEffort", "providerPolicyMode"} {
		after[field] = operation.Parameters[field]
	}
	after["providerAccounts"], after["providerCatalogPins"] = minimalAssistantRuntimeAccounts(candidates), candidates
	selectedPin, err := assistantRuntimeProfilePin(ctx, tx, assistantString(after, "runtimeProfileRef"), true)
	if err != nil {
		return operation, err
	}
	after["runtimeProfilePin"] = selectedPin
	currentPin, err := assistantRuntimeProfilePin(ctx, tx, view.Configuration.RuntimeProfileRef, false)
	if err != nil {
		return operation, err
	}
	before := assistantRuntimeBefore(target, current, view, currentPin)
	version := view.AgentVersion
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "AGENT", Ref: agentRef, Name: target.name, Version: &version}
	operation.Before, operation.Parameters, operation.After = before, after, cloneAssistantFields(after)
	operation.ExpectedVersion, operation.Selected, operation.Input = &version, true, nil
	if assistantRuntimeConfigurationUnchanged(before, after, view.Configuration.ProviderPolicy.AccountCandidates) {
		return operation, errAssistantRuntimeConfigurationNoChange
	}
	return operation, nil
}

// assistantRuntimeConfigurationCommand accepts only hydrated owner pins and routes each closed operation to its distinct internal command; payload locators grant no authority.
// assistantRuntimeConfigurationCommand принимает лишь подготовленные сервером owner pins и направляет каждую закрытую операцию в отдельную внутреннюю команду; ссылки payload не дают полномочий.
func assistantRuntimeConfigurationCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	if !onlyAssistantFields(operation.Input, "agentRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef", "runtimeProfileRef", "model", "reasoningEffort", "webSearchMode", "providerPolicyMode", "providerAccounts", "providerCatalogPins", "runtimeProfilePin", "targetKind", "expectedVersion") {
		return command.Command{}, errs.ErrInvalid
	}
	minimal, err := assistantRuntimeAccounts(operation.Input)
	if err != nil {
		return command.Command{}, err
	}
	raw, err := json.Marshal(operation.Input["providerCatalogPins"])
	if err != nil {
		return command.Command{}, errs.ErrInvalid
	}
	var pins []entity.ProviderAccountCandidate
	if json.Unmarshal(raw, &pins) != nil || len(pins) != len(minimal) {
		return command.Command{}, errs.ErrInvalid
	}
	for i := range pins {
		if pins[i].AccountRef != minimal[i].AccountRef || pins[i].Weight != minimal[i].Weight || !validRuntimeCatalogPin(pins[i]) {
			return command.Command{}, errs.ErrInvalid
		}
	}
	expected, ok := assistantInt64(operation.Input, "expectedVersion")
	if !ok || expected < 1 {
		return command.Command{}, errs.ErrInvalid
	}
	effort, ok := operation.Input["reasoningEffort"].(string)
	if !ok {
		return command.Command{}, errs.ErrInvalid
	}
	mode := ""
	if raw, present := operation.Input["webSearchMode"]; present {
		var valid bool
		mode, valid = raw.(string)
		if !valid || !runtimecontract.ValidWebSearchMode(mode) {
			return command.Command{}, errs.ErrInvalid
		}
	}
	payload := command.AssistantRuntimeConfigurationInput{Configuration: command.AgentRuntimeConfigurationInput{AgentRef: assistantString(operation.Input, "agentRef"), RuntimeProfileRef: assistantString(operation.Input, "runtimeProfileRef"), Model: assistantString(operation.Input, "model"), ProviderPolicyMode: assistantString(operation.Input, "providerPolicyMode"), ProviderAccounts: pins}, ReasoningEffort: effort,
		WebSearchMode: mode,
		TargetKind:    assistantString(operation.Input, "targetKind"),
		ScopeKind:     assistantString(operation.Input, "scopeKind"), OrganizationRef: assistantString(operation.Input, "organizationRef"), ProjectRef: assistantString(operation.Input, "projectRef"), AssistantProfileRef: assistantString(operation.Input, "assistantProfileRef")}
	profileRaw, err := json.Marshal(operation.Input["runtimeProfilePin"])
	if err != nil || decodeStrict(profileRaw, &payload.RuntimeProfilePin) != nil || payload.RuntimeProfilePin.Ref != payload.Configuration.RuntimeProfileRef || payload.RuntimeProfilePin.Version < 1 || payload.RuntimeProfilePin.RuntimeRevision == "" {
		return command.Command{}, errs.ErrInvalid
	}
	if !validModel(payload.Configuration.Model) || !validProviderPolicy(payload.Configuration.ProviderPolicyMode, minimal) {
		return command.Command{}, errs.ErrInvalid
	}
	kind := command.PublishAssistantRuntimeConfig
	if operation.Type == prepareAgentRuntimeConfiguration {
		if payload.TargetKind != "AGENT" || payload.ScopeKind != "PROJECT" || payload.ProjectRef == "" || payload.AssistantProfileRef != "" || operation.Input["assistantScope"] != nil {
			return command.Command{}, errs.ErrInvalid
		}
		kind = command.PublishPreparedAgentRuntimeConfig
	} else if payload.TargetKind != "" {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{Kind: kind, Payload: payload, Mutation: operationMutation(expected)}, nil
}

// operationMutation creates mutation metadata carrying only the expected version pointer; it does
// not assign an idempotency key, principal or authority.
// operationMutation создаёт mutation metadata только с указателем expected version; не назначает
// idempotency key, principal или authority.
func operationMutation(version int64) (mutation value.Mutation) {
	mutation.ExpectedVersion = &version
	return
}

// authorizeAssistantRuntimeConfiguration reruns the selected target resolver and exact owner lineage check before version/idempotency handling.
// authorizeAssistantRuntimeConfiguration повторяет выбранный resolver цели и проверку точной owner lineage до обработки версии и идемпотентности.
func (repository *Repository) authorizeAssistantRuntimeConfiguration(ctx context.Context, tx pgx.Tx, current scope, payload command.AssistantRuntimeConfigurationInput) (assistantConfigurationTarget, error) {
	target, err := repository.preparedRuntimeTarget(ctx, tx, current, payload.Configuration.AgentRef, payload.ProjectRef, payload.TargetKind == "AGENT")
	if err != nil {
		return target, err
	}
	if target.scopeKind != payload.ScopeKind || target.projectRef != payload.ProjectRef || target.profileRef != payload.AssistantProfileRef || current.organizationRef != payload.OrganizationRef {
		return target, errs.ErrNotFound
	}
	return target, nil
}

// assistantRuntimeConfigurationSnapshotMatches rejects current target, profile, catalog or overlay drift without repairing the immutable proposal.
// assistantRuntimeConfigurationSnapshotMatches отвергает дрейф цели, профиля, каталога или overlay, не исправляя неизменяемое предложение.
func (repository *Repository) assistantRuntimeConfigurationSnapshotMatches(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (bool, error) {
	planned, err := assistantRuntimeConfigurationCommand(operation)
	if err != nil {
		return false, err
	}
	payload := planned.Payload.(command.AssistantRuntimeConfigurationInput)
	target, err := repository.authorizeAssistantRuntimeConfiguration(ctx, tx, current, payload)
	if err != nil {
		return false, err
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, current, payload.Configuration.AgentRef)
	if err != nil {
		return false, err
	}
	currentPin, err := assistantRuntimeProfilePin(ctx, tx, view.Configuration.RuntimeProfileRef, false)
	if err != nil {
		return false, err
	}
	selectedPin, err := assistantRuntimeProfilePin(ctx, tx, payload.Configuration.RuntimeProfileRef, true)
	if err != nil {
		return false, err
	}
	if view.DraftOverlay != nil || selectedPin != payload.RuntimeProfilePin || view.AgentVersion != *planned.Mutation.ExpectedVersion || !assistantJSONEqual(assistantRuntimeBefore(target, current, view, currentPin), operation.Before) || !reflect.DeepEqual(operation.Parameters, operation.After) {
		return false, nil
	}
	overlay, err := assistantRuntimeOverlay(view.PublishedOverlay.Content, payload.ReasoningEffort, payload.WebSearchMode)
	if err != nil {
		return false, err
	}
	fresh, err := repository.prepareAssistantRuntimeCandidates(ctx, tx, current, operation.Parameters, overlay)
	if err != nil {
		return false, err
	}
	return assistantJSONEqual(fresh, operation.Parameters["providerCatalogPins"]), nil
}

// publishAssistantRuntimeConfiguration reauthorizes either closed target policy, then writes runtime and overlay under the caller-owned transaction; any later failure must roll both back.
// publishAssistantRuntimeConfiguration повторяет авторизацию одной из закрытых политик цели и записывает runtime с overlay в транзакции вызывающего; последующий сбой откатывает оба изменения.
func (repository *Repository) publishAssistantRuntimeConfiguration(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantRuntimeConfigurationInput)
	if !ok || (input.Kind == command.PublishPreparedAgentRuntimeConfig) != (payload.TargetKind == "AGENT") {
		return commandOutcome{}, errs.ErrInvalid
	}
	if _, err := repository.authorizeAssistantRuntimeConfiguration(ctx, tx, current, payload); err != nil {
		return commandOutcome{}, err
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, current, payload.Configuration.AgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if input.Mutation.ExpectedVersion == nil || view.AgentVersion != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	selectedPin, err := assistantRuntimeProfilePin(ctx, tx, payload.Configuration.RuntimeProfileRef, true)
	if err != nil {
		return commandOutcome{}, err
	}
	if view.DraftOverlay != nil || selectedPin != payload.RuntimeProfilePin {
		return commandOutcome{}, errs.ErrConflict
	}
	overlay, err := assistantRuntimeOverlay(view.PublishedOverlay.Content, payload.ReasoningEffort, payload.WebSearchMode)
	if err != nil {
		return commandOutcome{}, err
	}
	parameters := map[string]any{"runtimeProfileRef": payload.Configuration.RuntimeProfileRef, "model": payload.Configuration.Model, "providerPolicyMode": payload.Configuration.ProviderPolicyMode, "providerAccounts": minimalAssistantRuntimeAccounts(payload.Configuration.ProviderAccounts)}
	fresh, err := repository.prepareAssistantRuntimeCandidates(ctx, tx, current, parameters, overlay)
	if err != nil {
		return commandOutcome{}, err
	}
	if !assistantJSONEqual(fresh, payload.Configuration.ProviderAccounts) {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	configuration := payload.Configuration
	configuration.ProviderAccounts = append([]entity.ProviderAccountCandidate(nil), fresh...)
	for i := range configuration.ProviderAccounts {
		configuration.ProviderAccounts[i].DefaultReasoningEffort = ""
		configuration.ProviderAccounts[i].ModelCapabilityDigest = ""
	}
	published, err := repository.publishAgentRuntimeConfigurationWithOverlay(ctx, tx, current, command.Command{Kind: command.PublishAgentRuntimeConfig, Payload: configuration, Mutation: input.Mutation}, &overlay)
	if err != nil {
		return commandOutcome{}, err
	}
	if overlay == view.PublishedOverlay.Content {
		return published, nil
	}
	for _, kind := range []command.Kind{command.CreateConfigOverlayDraft, command.ValidateConfigOverlayDraft, command.PublishConfigOverlayDraft} {
		version := published.result.RuntimeConfiguration.AgentVersion
		published, err = repository.changeConfigOverlay(ctx, tx, current, command.Command{Kind: kind, Payload: command.ConfigOverlayInput{AgentRef: configuration.AgentRef, Content: overlay}, Mutation: operationMutation(version)})
		if err != nil {
			return commandOutcome{}, err
		}
	}
	return published, nil
}
