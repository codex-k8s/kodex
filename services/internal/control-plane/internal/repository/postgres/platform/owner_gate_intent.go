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

//go:embed sql/owner_gate_intent.sql
var queryOwnerGateIntent string

// Проекция вызывается после разрешения Gate; receipt и event повторяют тот же owner scope.
func (repository *Repository) projectGateIntent(ctx context.Context, runner queryRunner, current scope, gate *entity.OwnerGate, actorScoped bool) error {
	if gate == nil {
		return nil
	}
	var integration, delivery, completesRun bool
	var scopeJSON, input []byte
	var inputDigest, risk, approval string
	intent := &entity.IntegrationIntent{}
	var actorID any
	if current.actorID != "" {
		actorID = current.actorID
	}
	err := runner.QueryRow(ctx, queryOwnerGateIntent, current.organizationID, gate.Ref, current.role, actorID, actorScoped, current.authorityProjectID).Scan(
		&gate.SourceAttachmentSetRef, &integration, &delivery, &completesRun,
		&intent.ConnectionRef, &intent.ConnectionName, &intent.DefinitionKey, &intent.CapabilityKey, &intent.Operation,
		&intent.EffectKey, &intent.ResourceKind, &scopeJSON, &intent.ResourceScopeDigest, &input, &inputDigest, &risk, &approval,
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
		if err := repository.requireAccess(ctx, tx, current, "project.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: gate.ProjectRef}); err != nil {
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

const gatePreviewFieldBytes = 4 << 10
const gatePreviewContentBytes = 16 << 10

// Только поля известных операций shipped registry. Бинарные и вложенные значения
// не становятся текстовым preview; managed package не может расширить этот список.
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
