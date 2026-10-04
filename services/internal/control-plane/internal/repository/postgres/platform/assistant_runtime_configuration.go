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

var assistantRuntimeEditable = []string{"agentRef", "runtimeProfileRef", "model", "reasoningEffort", "providerPolicyMode", "providerAccounts"}
var assistantRuntimeOwnerFields = []string{"agentRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef"}

// Маркер относится только к полностью проверенной подготовке; обычный conflict
// и ошибки готовности не означают отсутствие изменений.
var errAssistantRuntimeConfigurationNoChange = errors.Join(errs.ErrConflict, errors.New("assistant runtime configuration is unchanged"))

func assistantRuntimeConfigurationUnchanged(before, after map[string]any, persisted []entity.ProviderAccountCandidate) bool {
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

func assistantReasoningOverlay(current, effort string) (string, error) {
	overlay, err := runtimecontract.ParseConfigOverlay(current)
	if err != nil {
		return "", errs.ErrConflict
	}
	overlay.ModelReasoningEffort = effort
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

func minimalAssistantRuntimeAccounts(candidates []entity.ProviderAccountCandidate) []map[string]any {
	result := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, map[string]any{"accountRef": candidate.AccountRef, "weight": candidate.Weight})
	}
	return result
}

func assistantRuntimeOwner(target assistantConfigurationTarget, current scope, agentRef string) map[string]any {
	assistantScope := "PROJECT"
	if target.scopeKind == "ORGANIZATION" {
		assistantScope = "SYSTEM"
	}
	return map[string]any{"agentRef": agentRef, "assistantScope": assistantScope, "scopeKind": target.scopeKind, "organizationRef": current.organizationRef, "projectRef": target.projectRef, "assistantProfileRef": target.profileRef}
}

//go:embed sql/assistant_configuration__profile_pin.sql
var queryAssistantConfigurationProfilePin string

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
	}
	return before
}

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
	overlay, err := assistantReasoningOverlay(view.PublishedOverlay.Content, effort)
	if err != nil {
		return operation, err
	}
	candidates, err := repository.prepareAssistantRuntimeCandidates(ctx, tx, current, operation.Parameters, overlay)
	if err != nil {
		return operation, err
	}
	after := assistantRuntimeOwner(target, current, agentRef)
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

func assistantRuntimeConfigurationCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	if !onlyAssistantFields(operation.Input, "agentRef", "assistantScope", "scopeKind", "organizationRef", "projectRef", "assistantProfileRef", "runtimeProfileRef", "model", "reasoningEffort", "providerPolicyMode", "providerAccounts", "providerCatalogPins", "runtimeProfilePin", "expectedVersion") {
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
	payload := command.AssistantRuntimeConfigurationInput{Configuration: command.AgentRuntimeConfigurationInput{AgentRef: assistantString(operation.Input, "agentRef"), RuntimeProfileRef: assistantString(operation.Input, "runtimeProfileRef"), Model: assistantString(operation.Input, "model"), ProviderPolicyMode: assistantString(operation.Input, "providerPolicyMode"), ProviderAccounts: pins}, ReasoningEffort: effort,
		ScopeKind: assistantString(operation.Input, "scopeKind"), OrganizationRef: assistantString(operation.Input, "organizationRef"), ProjectRef: assistantString(operation.Input, "projectRef"), AssistantProfileRef: assistantString(operation.Input, "assistantProfileRef")}
	profileRaw, err := json.Marshal(operation.Input["runtimeProfilePin"])
	if err != nil || decodeStrict(profileRaw, &payload.RuntimeProfilePin) != nil || payload.RuntimeProfilePin.Ref != payload.Configuration.RuntimeProfileRef || payload.RuntimeProfilePin.Version < 1 || payload.RuntimeProfilePin.RuntimeRevision == "" {
		return command.Command{}, errs.ErrInvalid
	}
	if !validModel(payload.Configuration.Model) || !validProviderPolicy(payload.Configuration.ProviderPolicyMode, minimal) {
		return command.Command{}, errs.ErrInvalid
	}
	return command.Command{Kind: command.PublishAssistantRuntimeConfig, Payload: payload, Mutation: operationMutation(expected)}, nil
}

func operationMutation(version int64) (mutation value.Mutation) {
	mutation.ExpectedVersion = &version
	return
}

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
	overlay, err := assistantReasoningOverlay(view.PublishedOverlay.Content, payload.ReasoningEffort)
	if err != nil {
		return false, err
	}
	fresh, err := repository.prepareAssistantRuntimeCandidates(ctx, tx, current, operation.Parameters, overlay)
	if err != nil {
		return false, err
	}
	return assistantJSONEqual(fresh, operation.Parameters["providerCatalogPins"]), nil
}

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
	overlay, err := assistantReasoningOverlay(view.PublishedOverlay.Content, payload.ReasoningEffort)
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
