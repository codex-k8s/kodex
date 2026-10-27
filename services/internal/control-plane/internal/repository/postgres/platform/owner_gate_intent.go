package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryOwnerGateIntent embeds exact gate/root/node/invocation/delivery lineage with owner scope,
// attachment source, pinned input and package metadata. Actor-scoped legacy membership visibility
// and authority-project restriction filter reads; command authority and preview disclosure are
// checked separately.
// queryOwnerGateIntent встраивает точные gate/root/node/invocation/delivery lineage с owner scope,
// attachment source, pinned input и package metadata. Actor-scoped legacy membership visibility и
// authority-project restriction фильтруют чтение; права команды и раскрытие preview проверяются
// отдельно.
//
//go:embed sql/owner_gate_intent.sql
var queryOwnerGateIntent string

// projectGateIntent projects only an already resolved gate, first verifying stored scope. Actor
// reads hide unavailable source attachments and scoped approval values without permission; events
// never expose scoped values. It verifies canonical input digest and exact package pins, allowing
// only terminal Forbidden-package history to remain redacted. Consequence count must match
// decisions. Errors can leave partial view changes; this projection neither resolves a gate nor
// grants authority.
// projectGateIntent дополняет лишь уже разрешённый gate, сначала сверяя stored scope. Actor reads
// скрывают недоступные source attachments и scoped approval values без права; events не раскрывают
// scoped values. Сверяет canonical input digest и exact package pins, разрешая только terminal
// history при Forbidden package остаться скрытой. Число consequences должно совпасть с decisions.
// Ошибка допускает частичные изменения view; проекция не решает gate и не выдаёт полномочия.
func (repository *Repository) projectGateIntent(ctx context.Context, runner queryRunner, current scope, gate *entity.OwnerGate, actorScoped bool) error {
	if gate == nil {
		return nil
	}
	if err := repository.projectOwnerGateScope(ctx, runner, current, gate, actorScoped); err != nil {
		return err
	}
	var integration, delivery, completesRun bool
	var scopeJSON, input []byte
	var inputDigest, risk, approval, definitionVersion, definitionDigest string
	var approvalScopePaths []string
	intent := &entity.IntegrationIntent{}
	var actorID any
	if current.actorID != "" {
		actorID = current.actorID
	}
	err := runner.QueryRow(ctx, queryOwnerGateIntent, current.organizationID, gate.Ref, current.role, actorID, actorScoped, current.authorityProjectID).Scan(
		&gate.SourceAttachmentSetRef, &integration, &delivery, &completesRun,
		&intent.ConnectionRef, &intent.ConnectionName, &intent.DefinitionKey, &intent.CapabilityKey, &intent.Operation,
		&intent.EffectKey, &intent.ResourceKind, &scopeJSON, &intent.ResourceScopeDigest, &input, &inputDigest, &risk, &approval,
		&definitionVersion, &definitionDigest, &approvalScopePaths,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if actorScoped && gate.SourceAttachmentSetRef != "" {
		tx, ok := runner.(pgx.Tx)
		if !ok {
			return errs.ErrUnavailable
		}
		permission, target := "project.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: gate.ProjectRef}
		if gate.ScopeKind == "ORGANIZATION" {
			permission, target = "organization.view", organizationTarget(gate.OrganizationRef)
		}
		if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
			if !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
				return err
			}
			gate.SourceAttachmentSetRef = ""
		}
	}
	gate.IntegrationIntent = nil
	if integration {
		if json.Unmarshal(scopeJSON, &intent.ResourceScope) != nil {
			return errs.ErrUnavailable
		}
		var previewErr error
		intent.EffectPreview, previewErr = integrationGatePreview(repository.integrationDefinitions, intent.DefinitionKey, intent.CapabilityKey, intent.Operation, input, inputDigest)
		if previewErr != nil {
			return previewErr
		}
		intent.EffectPreview["risk"], intent.EffectPreview["approvalPolicy"] = risk, approval
		if approval == string(integrationpackage.ApprovalHumanScoped) {
			// Для параметризованного согласования исходные аргументы не входят
			// в событие запуска и не раскрываются тем, кто видит Gate без права решения.
			showScopeValues := false
			if actorScoped {
				tx, ok := runner.(pgx.Tx)
				if !ok {
					return errs.ErrUnavailable
				}
				accessErr := repository.requireAccess(ctx, tx, current, "gate.resolve", entity.AccessScope{
					Kind: "RESOURCE_INSTANCE", ResourceKind: "OWNER_GATE", ResourceRef: gate.Ref,
				})
				if accessErr != nil && !errors.Is(accessErr, errs.ErrNotFound) && !errors.Is(accessErr, errs.ErrForbidden) {
					return accessErr
				}
				showScopeValues = accessErr == nil
			}
			if !showScopeValues {
				intent.EffectPreview["fields"] = []any{}
				intent.EffectPreview["contentComplete"] = false
			}
			definition, definitionErr := repository.integrationPackage(ctx, runner, current.organizationID,
				intent.ConnectionRef, intent.DefinitionKey, definitionVersion, definitionDigest)
			if definitionErr != nil {
				if !errors.Is(definitionErr, errs.ErrForbidden) || !terminalOwnerGateHistory(gate.State) {
					return definitionErr
				}
				// Завершённая история не исполняет retired пакет и не интерпретирует
				// его параметры по новой схеме. Owner scope и input digest уже проверены.
				intent.EffectPreview["fields"] = []any{}
				intent.EffectPreview["contentComplete"] = false
			} else {
				capability, ok := definition.Capability(intent.CapabilityKey)
				if !ok || capability.Operation != intent.Operation {
					return errs.ErrUnavailable
				}
				scopePreview, scopeErr := integrationScopedGatePreview(capability, input, approvalScopePaths, showScopeValues)
				if scopeErr != nil {
					return scopeErr
				}
				intent.EffectPreview["approvalScope"] = scopePreview
			}
		}
		gate.IntegrationIntent = intent
	}
	if len(gate.DecisionConsequences) == 0 {
		gate.DecisionConsequences = gateConsequences(gate.AllowedDecisions, integration, delivery, completesRun)
	}
	if len(gate.DecisionConsequences) != len(gate.AllowedDecisions) {
		return errs.ErrUnavailable
	}
	return nil
}

// terminalOwnerGateHistory recognizes the closed historical gate states eligible for redacted
// retired-package display, including CHANGES_REQUESTED. It describes gate history, not terminality
// of the owning run or permission to execute a retired definition.
// terminalOwnerGateHistory распознаёт закрытые исторические gate states для скрытого отображения
// retired package, включая CHANGES_REQUESTED. Описывает историю gate, а не конечность owning run и
// право исполнять retired definition.
func terminalOwnerGateHistory(state string) bool {
	switch state {
	case "APPROVED", "REJECTED", "CHANGES_REQUESTED", "CANCELLED", "EXPIRED":
		return true
	default:
		return false
	}
}

// gatePreviewFieldBytes caps displayed UTF-8 string content of one ordinary preview field at4KiB
// before safe truncation; it is not a bound for the whole encoded descriptor.
// gatePreviewFieldBytes ограничивает отображаемое UTF-8 строковое содержимое одного обычного
// preview field до4KiB перед безопасным усечением; не ограничивает полный encoded descriptor.
const gatePreviewFieldBytes = 4 << 10

// gatePreviewContentBytes provides a shared16KiB budget for displayed string content across a
// preview. Numeric/boolean values and descriptor metadata are handled separately; the value does
// not authorize disclosure.
// gatePreviewContentBytes задаёт общий16KiB бюджет отображаемого строкового содержимого preview.
// Числовые и boolean values и descriptor metadata обрабатываются отдельно; значение не разрешает
// раскрытие.
const gatePreviewContentBytes = 16 << 10

// integrationGatePreview bounds raw/canonical JSON and verifies SHA-256 of the parsed UseNumber
// map before exposing a shipped-registry capability preview. Only known fields get descriptors;
// attachments/nested data and guarded string names stay opaque, strings truncate safely
// within4/16KiB budgets and integer conversion is int64. Unknown package/operation yields an
// incomplete digest-only preview. This is not a strict EOF/schema decoder or an approval decision.
// integrationGatePreview ограничивает raw/canonical JSON и сверяет SHA-256 разобранной UseNumber
// map перед preview capability shipped registry. Лишь известные поля получают descriptors;
// attachments/вложенные данные и защищённые строковые имена остаются opaque, строки безопасно
// усекаются в бюджетах4/16KiB, integer conversion использует int64. Неизвестные package/operation
// дают неполное digest-only preview. Это не строгий EOF/schema decoder и не решение о допуске.
func integrationGatePreview(definitions map[string]integrationpackage.Package, key, capabilityKey, operation string, raw []byte, inputDigest string) (map[string]any, error) {
	if len(raw) > 1<<20 {
		return nil, errs.ErrUnavailable
	}
	var input map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&input) != nil || input == nil {
		return nil, errs.ErrUnavailable
	}
	canonical, err := json.Marshal(input)
	if err != nil || len(canonical) > 512<<10 {
		return nil, errs.ErrUnavailable
	}
	sum := sha256.Sum256(canonical)
	if hex.EncodeToString(sum[:]) != inputDigest {
		return nil, errs.ErrUnavailable
	}
	result := map[string]any{"inputDigest": inputDigest, "inputBytes": len(canonical), "contentComplete": false, "fields": []any{}}
	definition, ok := definitions[key]
	if !ok {
		return result, nil
	}
	capability, ok := definition.Capability(capabilityKey)
	if !ok || capability.Operation != operation {
		return result, nil
	}
	fields := []any{}
	remaining := gatePreviewContentBytes
	complete := true
	known := make(map[string]bool)
	for _, field := range capability.InputFields {
		known[field.Key] = true
		value, exists := input[field.Key]
		if !exists {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		descriptor := map[string]any{"key": field.Key, "type": field.Type, "bytes": len(encoded), "opaque": true}
		switch field.Key {
		case "attachments", "content_base64", "workflow_inputs":
			complete = false
		default:
			switch typed := value.(type) {
			case string:
				// Формат пакета не является разрешением вывести произвольный объект/JSON.
				if field.Type != "STRING" || strings.Contains(strings.ToLower(field.Key), "credential") || strings.Contains(strings.ToLower(field.Key), "password") || strings.Contains(strings.ToLower(field.Key), "token") || strings.Contains(strings.ToLower(field.Key), "header") {
					complete = false
					break
				}
				limit := min(gatePreviewFieldBytes, remaining)
				text := typed
				if len(text) > limit {
					text = text[:limit]
					for !utf8.ValidString(text) {
						text = text[:len(text)-1]
					}
				}
				remaining -= len(text)
				truncated := len(text) != len(typed)
				descriptor["value"], descriptor["opaque"], descriptor["truncated"] = text, false, truncated
				descriptor["bytes"] = len(typed)
				complete = complete && !truncated
			case json.Number:
				if field.Type == "INTEGER" {
					number, err := typed.Int64()
					if err != nil {
						return nil, errs.ErrUnavailable
					}
					descriptor["value"], descriptor["opaque"] = number, false
				} else {
					complete = false
				}
			case bool:
				if field.Type == "BOOLEAN" {
					descriptor["value"], descriptor["opaque"] = typed, false
				} else {
					complete = false
				}
			default:
				complete = false
			}
		}
		fields = append(fields, descriptor)
	}
	for field := range input {
		if !known[field] {
			complete = false
		}
	}
	result["fields"], result["contentComplete"] = fields, complete
	return result, nil
}

// gateConsequences maps known decisions to display-only external-effect/run-terminal flags and
// safe summaries using separate integration/delivery/completes-run context. Unknown decisions are
// skipped for caller cardinality rejection. It does not execute a transition or infer approval
// authority from the flags.
// gateConsequences сопоставляет известные decisions с отображаемыми external-effect/run-terminal
// flags и safe summaries по отдельным integration/delivery/completes-run признакам. Неизвестные
// decisions пропускаются для cardinality rejection caller. Не выполняет переход и не выводит права
// approval из flags.
func gateConsequences(decisions []string, integration, delivery, completesRun bool) []entity.OwnerGateDecisionConsequence {
	result := make([]entity.OwnerGateDecisionConsequence, 0, len(decisions))
	for _, decision := range decisions {
		consequence := entity.OwnerGateDecisionConsequence{Decision: decision}
		switch decision {
		case "APPROVE":
			consequence.ExecutesExternalEffect = integration || delivery
			consequence.TerminalForRun = completesRun && !delivery
			consequence.SafeSummary = "i18n:GATE_CONSEQUENCE_CONTINUE"
			if integration || delivery {
				consequence.SafeSummary = "i18n:GATE_CONSEQUENCE_EXTERNAL_EFFECT"
			}
		case "REJECT":
			consequence.TerminalForRun = !integration && !delivery
			consequence.SafeSummary = "i18n:GATE_CONSEQUENCE_REJECT_RUN"
			if integration || delivery {
				consequence.SafeSummary = "i18n:GATE_CONSEQUENCE_REJECT_EFFECT"
			}
		case "CANCEL":
			consequence.TerminalForRun = !integration && !delivery
			consequence.SafeSummary = "i18n:GATE_CONSEQUENCE_CANCEL_RUN"
			if integration || delivery {
				consequence.SafeSummary = "i18n:GATE_CONSEQUENCE_CANCEL_EFFECT"
			}
		case "REQUEST_CHANGES":
			consequence.SafeSummary = "i18n:GATE_CONSEQUENCE_REQUEST_CHANGES"
		default:
			continue
		}
		result = append(result, consequence)
	}
	return result
}
