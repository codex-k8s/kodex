package platform

import (
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"reflect"
	"strings"
	"testing"
)

// TestProjectAssistantConnectionCommandClosedPins Checks the real operation mapper chooses the
// specialized project-assistant connection command and rejects missing owner/version pins, extra
// authority/credential-like fields and malformed scope/version/configuration values. Uses
// synthetic public data without DB access or actual connection effects.
// TestProjectAssistantConnectionCommandClosedPins Проверяет настоящий mapper: специализированная
// команда соединения ассистента, отказ отсутствующим привязкам владельца и версий, лишним полям
// полномочий или credential, неверным scope, версиям и конфигурации. Использует синтетические
// публичные данные без БД и реального соединения.
func TestProjectAssistantConnectionCommandClosedPins(t *testing.T) {
	input := map[string]any{"projectAssistantRef": "agt_fixture12345", "assistantScope": "PROJECT", "scopeKind": "ORGANIZATION", "organizationRef": "org_fixture12345", "projectRef": "prj_fixture12345", "assistantProfileRef": "asstp_fixture12345", "agentVersion": int64(2), "profileVersion": int64(1), "definitionVersion": "2.3.1", "definitionDigest": strings.Repeat("a", 64), "definitionKey": "github", "name": "Own repository", "publicConfiguration": map[string]any{"owner": "fixture", "repository": "repository"}, "expectedVersion": int64(2)}
	op := entity.AssistantPlanOperation{Type: prepareProjectAssistantConnection, Input: input}
	mapped, err := assistantOperationCommand(op)
	if err != nil || mapped.Kind != command.CreateProjectAssistantIntegrationConnection {
		t.Fatalf("closed mapper: %v", err)
	}
	for _, field := range []string{"projectRef", "organizationRef", "projectAssistantRef", "assistantProfileRef", "definitionDigest", "profileVersion", "agentVersion"} {
		bad := op
		bad.Input = cloneAssistantFields(input)
		delete(bad.Input, field)
		if _, err := assistantOperationCommand(bad); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("missing %s accepted", field)
		}
	}
	for _, field := range []string{"ownerID", "credentials", "scope", "agentRef"} {
		bad := op
		bad.Input = cloneAssistantFields(input)
		bad.Input[field] = ""
		if _, err := assistantOperationCommand(bad); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("extra %s accepted", field)
		}
	}
	for _, change := range []struct {
		field string
		value any
	}{{"profileVersion", int64(0)}, {"agentVersion", int64(3)}, {"scopeKind", "PROJECT"}, {"assistantScope", "SYSTEM"}, {"publicConfiguration", map[string]any{"token": true}}} {
		bad := op
		bad.Input = cloneAssistantFields(input)
		bad.Input[change.field] = change.value
		if _, err := assistantOperationCommand(bad); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("malformed %s accepted", change.field)
		}
	}
}

// TestAssistantHydrationRequestKeepsClosedParameterSubset checks connection/grant field lists,
// missing keys and an empty list against literal parameter maps while preserving the rest of the
// plan envelope. The outer parameter map is independent; nested configuration stays shallow as
// before. Protected/unknown parameter pins are not carried into fresh hydration requests.
// TestAssistantHydrationRequestKeepsClosedParameterSubset проверяет списки соединения и grant,
// отсутствующие ключи и пустой список по буквальным картам, сохраняя остальные поля плана.
// Внешняя карта параметров независима, вложенная конфигурация остаётся общей как прежде.
// Защищённые и неизвестные привязки Parameters не переходят в запрос свежего чтения.
func TestAssistantHydrationRequestKeepsClosedParameterSubset(t *testing.T) {
	configuration := map[string]any{"mode": "public"}
	for _, test := range []struct {
		name        string
		fields      []string
		input, want map[string]any
	}{
		{name: "connection", fields: projectConnectionEditable,
			input: map[string]any{"projectAssistantRef": "assistant-public", "name": "Public connection", "publicConfiguration": configuration, "agentVersion": int64(9), "unexpectedOwner": "drop"},
			want:  map[string]any{"projectAssistantRef": "assistant-public", "definitionKey": nil, "name": "Public connection", "publicConfiguration": configuration}},
		{name: "grant", fields: assistantProjectGrantEditable,
			input: map[string]any{"projectAssistantRef": "assistant-public", "connectionRef": "connection-public", "capabilityKey": "fixture.read", "enabled": true, "approvalPolicy": "NONE", "approvalScopePaths": []string{}, "grantVersion": int64(9), "unexpectedOwner": "drop"},
			want:  map[string]any{"projectAssistantRef": "assistant-public", "connectionRef": "connection-public", "capabilityKey": "fixture.read", "enabled": true, "approvalPolicy": "NONE", "approvalScopePaths": []string{}}},
		{name: "system-grant", fields: assistantSystemGrantEditable,
			input: map[string]any{"connectionRef": "connection-public", "capabilityKey": "fixture.read", "enabled": true, "approvalPolicy": "NONE", "approvalScopePaths": []string{}, "projectAssistantRef": "drop", "systemAssistantRef": "drop-owner"},
			want:  map[string]any{"connectionRef": "connection-public", "capabilityKey": "fixture.read", "enabled": true, "approvalPolicy": "NONE", "approvalScopePaths": []string{}}},
		{name: "nil-parameters", fields: projectConnectionEditable, want: map[string]any{"projectAssistantRef": nil, "definitionKey": nil, "name": nil, "publicConfiguration": nil}},
		{name: "empty-list", input: map[string]any{"unexpectedOwner": "drop"}, want: map[string]any{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			version := int64(3)
			operation := entity.AssistantPlanOperation{Key: "operation", Type: "TYPE", Action: "UPDATE", Selected: true,
				Parameters: test.input, Before: map[string]any{"owner": "before"}, After: map[string]any{"owner": "after"},
				Input: map[string]any{"local": "retained"}, ExpectedVersion: &version,
				Target: entity.AssistantPlanTarget{Kind: "OWNER", Ref: "owner-public", Version: &version}}
			want := operation
			want.Parameters = test.want
			got := assistantHydrationRequest(operation, test.fields)
			if !reflect.DeepEqual(got, want) || got.Parameters == nil || got.ExpectedVersion != operation.ExpectedVersion || got.Target.Version != operation.Target.Version {
				t.Fatal("hydration request changed closed fields, shape or envelope")
			}
			got.Parameters["local-new"] = "request-only"
			if _, present := operation.Parameters["local-new"]; present {
				t.Fatal("hydration request aliases outer parameter map")
			}
			if test.name == "connection" {
				got.Parameters["publicConfiguration"].(map[string]any)["mode"] = "changed"
				if operation.Parameters["publicConfiguration"].(map[string]any)["mode"] != "changed" {
					t.Fatal("hydration request changed historical shallow nested values")
				}
				configuration["mode"] = "public"
			}
		})
	}
}
