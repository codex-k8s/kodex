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
var assistantRuntimeOwnerFields = []string{"agentRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef"}

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

// assistantRuntimeOwner builds the server-owned assistant identity map from the resolved
// target/current organization, translating organization scope to SYSTEM and other target scope to
// PROJECT.
// assistantRuntimeOwner строит серверную assistant identity map по разрешённому target/current
// organization, преобразуя organization scope в SYSTEM, остальные target scope в PROJECT.
func assistantRuntimeOwner(target assistantConfigurationTarget, current scope, agentRef string) map[string]any {
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

// hydrateAssistantRuntimeConfiguration accepts only editable fields, resolves authoritative
// assistant target/current view and rejects existing overlay drafts. It prepares exact
// model/account/profile pins and before/after/version snapshots, preserving omitted search mode; a
// fully validated identical proposal returns the dedicated no-change marker without persistence.
// hydrateAssistantRuntimeConfiguration принимает лишь editable fields, разрешает авторитетные
// assistant target/current view и отклоняет существующий overlay draft. Готовит точные
// model/account/profile pins и before/after/version snapshots, сохраняя пропущенный search mode;
// полностью проверенное идентичное предложение возвращает отдельный no-change marker без
// сохранения.
func (repository *Repository) hydrateAssistantRuntimeConfiguration(ctx context.Context, tx pgx.Tx, current scope, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(operation.Parameters, assistantRuntimeEditable...) {
		return operation, errs.ErrInvalid
	}
	agentRef := assistantString(operation.Parameters, "agentRef")
	target, err := repository.assistantConfigurationTarget(ctx, tx, current, agentRef)
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

// assistantRuntimeConfigurationCommand converts closed frozen input to
// PublishAssistantRuntimeConfig after validating sorted account/weight matches, catalog pins,
// positive expected version, search mode, exact strict profile pin and model/policy shape.
// Actor/tenant authority is still resolved by the command owner.
// assistantRuntimeConfigurationCommand преобразует закрытый frozen input в
// PublishAssistantRuntimeConfig после проверки совпадения sorted account/weight, catalog pins,
// положительной expected version, search mode, точного strict profile pin и model/policy shape.
// Actor/tenant authority по-прежнему разрешает command owner.
func assistantRuntimeConfigurationCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	if !onlyAssistantFields(operation.Input, "agentRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef", "runtimeProfileRef", "model", "reasoningEffort", "webSearchMode", "providerPolicyMode", "providerAccounts", "providerCatalogPins", "runtimeProfilePin", "expectedVersion") {
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
		ScopeKind:     assistantString(operation.Input, "scopeKind"), OrganizationRef: assistantString(operation.Input, "organizationRef"), ProjectRef: assistantString(operation.Input, "projectRef"), AssistantProfileRef: assistantString(operation.Input, "assistantProfileRef")}
	profileRaw, err := json.Marshal(operation.Input["runtimeProfilePin"])
	if err != nil || decodeStrict(profileRaw, &payload.RuntimeProfilePin) != nil || payload.RuntimeProfilePin.Ref != payload.Configuration.RuntimeProfileRef || payload.RuntimeProfilePin.Version < 1 || payload.RuntimeProfilePin.RuntimeRevision == "" {
		return command.Command{}, errs.ErrInvalid
	}
	if !validModel(payload.Configuration.Model) || !validProviderPolicy(payload.Configuration.ProviderPolicyMode, minimal) {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{Kind: command.PublishAssistantRuntimeConfig, Payload: payload, Mutation: operationMutation(expected)}, nil
}

// operationMutation creates mutation metadata carrying only the expected version pointer; it does
// not assign an idempotency key, principal or authority.
// operationMutation создаёт mutation metadata только с указателем expected version; не назначает
// idempotency key, principal или authority.
func operationMutation(version int64) (mutation value.Mutation) {
	mutation.ExpectedVersion = &version
	return
}

// authorizeAssistantRuntimeConfiguration resolves the canonical mutation target and requires
// payload scope/project/profile/organization pins to match authoritative state, hiding mismatches
// as NotFound.
// authorizeAssistantRuntimeConfiguration разрешает канонический mutation target и требует
// совпадения payload scope/project/profile/organization pins с авторитетным состоянием, скрывая
// несовпадения как NotFound.
func (repository *Repository) authorizeAssistantRuntimeConfiguration(ctx context.Context, tx pgx.Tx, current scope, payload command.AssistantRuntimeConfigurationInput) (assistantConfigurationTarget, error) {
	target, err := repository.assistantConfigurationTarget(ctx, tx, current, payload.Configuration.AgentRef)
	if err != nil {
		return target, err
	}
	if target.scopeKind != payload.ScopeKind || target.projectRef != payload.ProjectRef || target.profileRef != payload.AssistantProfileRef || current.organizationRef != payload.OrganizationRef {
		return target, errs.ErrNotFound
	}
	return target, nil
}

// assistantRuntimeConfigurationSnapshotMatches decodes the frozen command, revalidates authority
// and both profile pins, then compares agent version, absence of draft, exact before snapshot and
// parameters/after equality. It recomputes overlay/account catalogs and compares fresh pins
// without applying configuration.
// assistantRuntimeConfigurationSnapshotMatches декодирует frozen command, повторно проверяет
// authority и оба profile pins, затем сравнивает agent version, отсутствие draft, точный before
// snapshot и равенство parameters/after. Повторно вычисляет overlay/account catalogs и сравнивает
// свежие pins без применения configuration.
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

// publishAssistantRuntimeConfiguration checks typed payload, canonical authority, OCC, enabled
// profile pin and no draft, then recomputes exact account/overlay eligibility. It clears only
// nonpersisted capability fields on a copy, publishes model configuration and, if overlay bytes
// changed, runs draft/create-validate-publish in the same caller transaction; no commit or
// provider execution occurs here.
// publishAssistantRuntimeConfiguration проверяет typed payload, канонические authority, OCC,
// enabled profile pin и отсутствие draft, затем заново вычисляет точную account/overlay
// eligibility. Очищает лишь несохраняемые capability fields на копии, публикует model
// configuration и при изменении overlay bytes выполняет draft/create-validate-publish в той же
// транзакции caller; commit или provider execution здесь нет.
func (repository *Repository) publishAssistantRuntimeConfiguration(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantRuntimeConfigurationInput)
	if !ok {
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
