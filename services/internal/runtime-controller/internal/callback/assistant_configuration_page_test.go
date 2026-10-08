package callback

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func assistantConfigurationPageFixture(t *testing.T, kind string) (map[string]any, []byte, string) {
	t.Helper()
	snapshot := map[string]any{"instructions": strings.Repeat("Я😀中é\n", 1000), "steps": []any{map[string]any{"name": "Полный шаг", "dependsOn": []any{"первый"}}}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	key, ref := "workflow_configuration", "workflow_ref"
	if kind == "AGENT_CONFIGURATION" {
		key, ref = "agent_configuration", "agent_ref"
	}
	return map[string]any{"kind": kind, "assistant_ref": "agt_fixture123", "organization_ref": "org_fixture123", "project_ref": "prj_fixture123", "entries": []any{}, key: map[string]any{ref: "res_fixture123", "project_ref": "prj_fixture123", "version": int64(7), "configuration_sha256": digest, "configuration": snapshot}}, raw, digest
}

func TestAssistantConfigurationPageReconstructsExactUTF8Source(t *testing.T) {
	for _, kind := range []string{"WORKFLOW_CONFIGURATION", "AGENT_CONFIGURATION"} {
		t.Run(kind, func(t *testing.T) {
			configuration, source, digest := assistantConfigurationPageFixture(t, kind)
			key := "workflow_configuration"
			if kind == "AGENT_CONFIGURATION" {
				key = "agent_configuration"
			}
			before, _ := json.Marshal(configuration)
			var reconstructed []byte
			var offset int64
			for count := 0; ; count++ {
				if count > len(source)/4+1 {
					t.Fatal("страницы не продвигаются")
				}
				result, err := pageAssistantConfigurationSnapshot(configuration, assistantConfigurationPageRequest{offset: offset, maximum: 17, digest: digest})
				if err != nil {
					t.Fatal(err)
				}
				inner := result[key].(map[string]any)
				page := inner["configuration_page"].(map[string]any)
				text := page["text"].(string)
				sum := sha256.Sum256([]byte(text))
				if !utf8.ValidString(text) || len(text) > 17 || page["size_bytes"] != len(source) || page["offset_bytes"] != offset || page["next_offset_bytes"] != offset+int64(len(text)) || page["page_sha256"] != hex.EncodeToString(sum[:]) {
					t.Fatal("страница потеряла размер, UTF-8 или дайджест")
				}
				if _, exposed := inner["configuration"]; exposed || inner["configuration_sha256"] != digest || inner["version"] != int64(7) || result["project_ref"] != configuration["project_ref"] {
					t.Fatal("страница потеряла pins или выдала полный снимок")
				}
				reconstructed = append(reconstructed, text...)
				offset = page["next_offset_bytes"].(int64)
				if page["eof"].(bool) {
					break
				}
				if len(text) == 0 {
					t.Fatal("непустой остаток без продвижения")
				}
			}
			if !bytes.Equal(reconstructed, source) {
				t.Fatal("EOF не покрывает точные байты полного снимка")
			}
			result, err := pageAssistantConfigurationSnapshot(configuration, assistantConfigurationPageRequest{offset: int64(len(source)), maximum: 4, digest: digest})
			if err != nil {
				t.Fatal(err)
			}
			page := result[key].(map[string]any)["configuration_page"].(map[string]any)
			if page["text"] != "" || page["eof"] != true || page["next_offset_bytes"] != int64(len(source)) {
				t.Fatal("точный EOF не вернул пустую конечную страницу")
			}
			after, _ := json.Marshal(configuration)
			if !bytes.Equal(before, after) {
				t.Fatal("исходная проверенная модель изменена")
			}
		})
	}
}

func TestAssistantConfigurationPageParserClosedInputs(t *testing.T) {
	digest := strings.Repeat("a", 64)
	page, err := parseAssistantConfigurationPage(map[string]any{"kind": "WORKFLOW_CONFIGURATION"}, "WORKFLOW_CONFIGURATION")
	if err != nil || page != (assistantConfigurationPageRequest{maximum: 16384}) {
		t.Fatal("неверные значения по умолчанию")
	}
	valid := map[string]any{"kind": "AGENT_CONFIGURATION", "configuration_offset_bytes": float64(10), "maximum_bytes": float64(4), "configuration_sha256": digest}
	page, err = parseAssistantConfigurationPage(valid, "AGENT_CONFIGURATION")
	if err != nil || page.offset != 10 || page.maximum != 4 || page.digest != digest {
		t.Fatal("валидные точные координаты не приняты")
	}
	for _, item := range []struct {
		key   string
		value any
	}{
		{"configuration_offset_bytes", -1}, {"configuration_offset_bytes", 0.5}, {"configuration_offset_bytes", math.NaN()}, {"configuration_offset_bytes", math.Inf(1)}, {"configuration_offset_bytes", maximumAssistantCurrentConfigurationBytes + 1}, {"configuration_offset_bytes", "0"},
		{"maximum_bytes", 3}, {"maximum_bytes", 16385}, {"maximum_bytes", 4.5}, {"configuration_sha256", strings.Repeat("A", 64)}, {"configuration_sha256", ""}, {"configuration_sha256", 7}, {"unknown", "PRIVATE_SENTINEL"},
	} {
		t.Run(item.key+"/"+reflect.TypeOf(item.value).String(), func(t *testing.T) {
			selector := map[string]any{}
			for key, value := range valid {
				selector[key] = value
			}
			selector[item.key] = item.value
			if _, err := parseAssistantConfigurationPage(selector, "AGENT_CONFIGURATION"); !errors.Is(err, errAssistantConfigurationPage) || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
				t.Fatal("недопустимый ввод принят либо раскрыт")
			}
		})
	}
	delete(valid, "configuration_sha256")
	for _, maximum := range []int{4, 4096, 16384} {
		page, err := parseAssistantConfigurationPage(map[string]any{"maximum_bytes": maximum}, "WORKFLOW_CONFIGURATION")
		if err != nil || page.maximum != int64(maximum) {
			t.Fatal("допустимый размер страницы не принят")
		}
	}
	if _, err := parseAssistantConfigurationPage(valid, "AGENT_CONFIGURATION"); err == nil {
		t.Fatal("продолжение без дайджеста принято")
	}
	for _, kind := range []string{"CURRENT_CONFIGURATION", "ASSISTANTS", "CREDENTIALS"} {
		if _, err := parseAssistantConfigurationPage(map[string]any{"maximum_bytes": 4096}, kind); err == nil {
			t.Fatal("страницы разрешены для чужого вида")
		}
	}
}

func TestAssistantConfigurationPageRejectsMismatchWithoutContent(t *testing.T) {
	for _, scenario := range []string{"offset beyond EOF", "offset inside rune", "missing continuation digest", "wrong expected digest", "wrong source digest", "mixed kind", "unknown kind", "invalid UTF8", "oversized source", "invalid maximum"} {
		t.Run(scenario, func(t *testing.T) {
			configuration, source, digest := assistantConfigurationPageFixture(t, "WORKFLOW_CONFIGURATION")
			inner := configuration["workflow_configuration"].(map[string]any)
			page := assistantConfigurationPageRequest{maximum: 4096, digest: digest}
			switch scenario {
			case "offset beyond EOF":
				page.offset = int64(len(source) + 1)
			case "offset inside rune":
				page.offset = int64(bytes.Index(source, []byte("Я")) + 1)
			case "missing continuation digest":
				page.offset, page.digest = 1, ""
			case "wrong expected digest":
				page.digest = strings.Repeat("b", 64)
			case "wrong source digest":
				inner["configuration_sha256"] = strings.Repeat("c", 64)
			case "mixed kind":
				configuration["agent_configuration"] = inner
			case "unknown kind":
				configuration["kind"] = "CREDENTIALS"
			case "invalid UTF8", "oversized source":
				snapshot := inner["configuration"].(map[string]any)
				if scenario == "invalid UTF8" {
					snapshot["instructions"] = "PRIVATE_SENTINEL" + string([]byte{0xff})
				} else {
					snapshot["instructions"] = strings.Repeat("x", maximumAssistantCurrentConfigurationBytes)
				}
				raw, _ := json.Marshal(snapshot)
				hash := sha256.Sum256(raw)
				page.digest = hex.EncodeToString(hash[:])
				inner["configuration_sha256"] = page.digest
			case "invalid maximum":
				page.maximum = 3
			}
			result, err := pageAssistantConfigurationSnapshot(configuration, page)
			if result != nil || !errors.Is(err, errAssistantConfigurationPage) || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
				t.Fatal("ошибка выдала снимок или скрылась")
			}
		})
	}
}

func TestAssistantConfigurationPageAdaptiveEncodedBudget(t *testing.T) {
	if maximumAssistantCurrentConfigurationBytes != 1<<20 {
		t.Fatal("полный исходный snapshot расширен")
	}
	for _, kind := range []string{"WORKFLOW_CONFIGURATION", "AGENT_CONFIGURATION"} {
		t.Run(kind, func(t *testing.T) {
			configuration, _, _ := assistantConfigurationPageFixture(t, kind)
			key := "workflow_configuration"
			if kind == "AGENT_CONFIGURATION" {
				key = "agent_configuration"
			}
			inner := configuration[key].(map[string]any)
			inner["configuration"].(map[string]any)["instructions"] = strings.Repeat("\"\\\n<>&😀", 5000)
			source, _ := json.Marshal(inner["configuration"])
			hash := sha256.Sum256(source)
			digest := hex.EncodeToString(hash[:])
			inner["configuration_sha256"] = digest
			before, _ := json.Marshal(configuration)
			var full []byte
			for count := 0; count <= len(source)/4; count++ {
				// Меньший внутренний budget проверяет shrink; MCP caller его не задаёт.
				catalog, err := boundedAssistantConfigurationPageCatalog(configuration, assistantConfigurationPageRequest{offset: int64(len(full)), maximum: 16384, digest: digest}, "prj_fixture123", 8192)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(catalog)
				result := catalog["assistant_configuration_catalog"].(map[string]any)[key].(map[string]any)
				page := result["configuration_page"].(map[string]any)
				text := page["text"].(string)
				pageHash := sha256.Sum256([]byte(text))
				if err != nil || len(encoded) > 8192 || !utf8.ValidString(text) || page["offset_bytes"] != int64(len(full)) || page["next_offset_bytes"] != int64(len(full)+len(text)) || page["page_sha256"] != hex.EncodeToString(pageHash[:]) || result["configuration_sha256"] != digest || result["version"] != int64(7) || page["size_bytes"] != len(source) {
					t.Fatal("адаптивная страница потеряла budget, границы или pins")
				}
				if count == 0 && len(text) >= 16384 {
					t.Fatal("escaping не уменьшил страницу при ограниченном внутреннем budget")
				}
				full = append(full, text...)
				if page["eof"] == true {
					break
				}
				if len(text) == 0 {
					t.Fatal("адаптивная страница не продвигается")
				}
			}
			after, _ := json.Marshal(configuration)
			if !bytes.Equal(full, source) || !bytes.Equal(before, after) {
				t.Fatal("адаптивное чтение изменило source или не дошло до EOF")
			}
			if result, err := boundedAssistantConfigurationPageCatalog(configuration, assistantConfigurationPageRequest{maximum: 16384}, "prj_fixture123", 1); result != nil || !errors.Is(err, errAssistantConfigurationPage) {
				t.Fatal("невмещающийся envelope выдан частично")
			}
		})
	}
}

func TestAssistantConfigurationPageSchemaBudget(t *testing.T) {
	input, _, _ := assistantWorkflowConfigurationFixture(t)
	properties := assistantConfigurationCatalogInputSchema(input)["properties"].(map[string]any)
	maximum := properties["maximum_bytes"].(map[string]any)
	if maximum["minimum"] != 4 || maximum["maximum"] != 16384 || maximum["default"] != 16384 || !strings.Contains(configurationCatalogTool(input)["description"].(string), "4..16384") || !strings.Contains(assistantCatalogReadInputGuidance, "4..16384") {
		t.Fatal("schema, parser и guidance расходятся по размеру страницы")
	}
	input.AssistantContext = nil
	if _, exposed := assistantConfigurationCatalogInputSchema(input)["properties"].(map[string]any)["maximum_bytes"]; exposed {
		t.Fatal("страницы раскрыты вне доступного exact resource context")
	}
}
