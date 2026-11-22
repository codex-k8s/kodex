package internalrpcauth

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// exactJSONFixture models plain optional request fields without interpreting omitted values as null.
// exactJSONFixture описывает обычные необязательные поля запроса, не приравнивая отсутствие к null.
type exactJSONFixture struct {
	Enabled bool             `json:"enabled"`
	Label   *string          `json:"label,omitempty"`
	Items   []exactJSONChild `json:"items,omitempty"`
	Nested  *exactJSONChild  `json:"nested,omitempty"`
}

// exactJSONChild supplies nested tags and a bounded scalar for standard decoder overflow checks.
// exactJSONChild задаёт вложенные теги и ограниченный scalar для проверки overflow стандартным decoder.
type exactJSONChild struct {
	Name  string `json:"name"`
	Count int8   `json:"count"`
}

// TestDecodeExactJSONRejectsMalformedUTF8 proves standard replacement cannot normalize invalid
// bytes into an accepted DTO value and every failed exact decode clears the previous target.
// TestDecodeExactJSONRejectsMalformedUTF8 доказывает, что standard replacement не превращает
// invalid bytes в accepted DTO value; каждый failed exact decode очищает прежний target.
func TestDecodeExactJSONRejectsMalformedUTF8(t *testing.T) {
	malformed := append([]byte(`{"label":"`), 0xff)
	malformed = append(malformed, []byte(`"}`)...)
	var standard exactJSONFixture
	if DecodeStrictJSON(malformed, &standard) != nil || standard.Label == nil || *standard.Label != "\ufffd" {
		t.Fatal("original standard decoder UTF-8 replacement counterfactual changed")
	}
	for _, raw := range [][]byte{malformed, append([]byte(`{"enabled":true,"`), 0xff, '"', ':', '1', '}')} {
		label := "previous"
		value := exactJSONFixture{Enabled: true, Label: &label}
		if DecodeExactJSON(raw, &value) == nil || value.Enabled || value.Label != nil {
			t.Fatal("malformed UTF-8 was normalized into exact DTO semantics")
		}
	}
	var valid exactJSONFixture
	if DecodeExactJSON([]byte(`{"label":"Проверка 😀"}`), &valid) != nil || valid.Label == nil || *valid.Label != "Проверка 😀" {
		t.Fatal("valid Unicode was narrowed by UTF-8 admission")
	}
}

// exactJSONCustomText represents a custom standard decoder path unsupported by plain DTO checks.
// exactJSONCustomText задаёт custom standard decoder path, запрещённый проверкой обычных DTO.
type exactJSONCustomText string

// UnmarshalText must never run because complete type preflight rejects custom text representations.
// UnmarshalText не должен запускаться: полная проверка типов отклоняет custom text representation.
func (*exactJSONCustomText) UnmarshalText([]byte) error {
	panic("unsupported custom text decoder executed")
}

// TestDecodeExactJSONRejectsAmbiguity demonstrates the original case/null counterfactual and checks
// exact nested keys, duplicates, trailing data, overflow and clearing after every rejected document.
// TestDecodeExactJSONRejectsAmbiguity воспроизводит исходный case/null контрпример и проверяет точные
// вложенные ключи, повторы, хвост, overflow и очистку после каждого отклонённого документа.
func TestDecodeExactJSONRejectsAmbiguity(t *testing.T) {
	var formerlyAccepted exactJSONFixture
	if DecodeStrictJSON([]byte(`{"Enabled":true,"label":null}`), &formerlyAccepted) != nil || !formerlyAccepted.Enabled {
		t.Fatal("original standard decoder counterfactual changed")
	}
	for name, raw := range map[string]string{
		"case alias":      `{"Enabled":true}`,
		"case duplicate":  `{"enabled":false,"Enabled":true}`,
		"duplicate":       `{"enabled":false,"enabled":true}`,
		"unknown":         `{"owner":"private-untrusted-value"}`,
		"required null":   `{"enabled":null}`,
		"optional null":   `{"label":null}`,
		"nested null":     `{"nested":null}`,
		"nested alias":    `{"nested":{"Name":"other"}}`,
		"array alias":     `{"items":[{"NAME":"other"}]}`,
		"array duplicate": `{"items":[{"name":"a","name":"b"}]}`,
		"array null":      `{"items":null}`,
		"element null":    `{"items":[null]}`,
		"overflow":        `{"nested":{"count":128}}`,
		"wrong scalar":    `{"enabled":"true"}`,
		"trailing":        `{} {}`,
		"root null":       `null`,
		"root array":      `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			value := exactJSONFixture{Enabled: true}
			err := DecodeExactJSON([]byte(raw), &value)
			if err == nil || !reflect.DeepEqual(value, exactJSONFixture{}) || strings.Contains(err.Error(), "private-untrusted-value") || strings.Contains(err.Error(), "owner") {
				t.Fatal("ambiguous JSON was accepted, retained a partial value or exposed input")
			}
		})
	}
}

// TestDecodeExactJSONPreservesOmissionAndExplicitNullability permits missing optional fields,
// explicit empty collections and only caller-declared nullable paths; unrelated null stays invalid.
// TestDecodeExactJSONPreservesOmissionAndExplicitNullability допускает отсутствие необязательных
// полей, пустые коллекции и только явно nullable paths; посторонний null остаётся ошибкой.
func TestDecodeExactJSONPreservesOmissionAndExplicitNullability(t *testing.T) {
	for _, raw := range []string{`{}`, `{"enabled":false,"label":"","items":[]}`, `{"items":[{"name":"a","count":127}]}`, `{"nested":{"count":-128}}`} {
		var value exactJSONFixture
		if err := DecodeExactJSON([]byte(raw), &value); err != nil {
			t.Fatalf("ordinary exact document rejected: %v", err)
		}
	}
	var value exactJSONFixture
	if DecodeExactJSON([]byte(`{"items":null}`), &value, "items") != nil || DecodeExactJSON([]byte(`{"label":null}`), &value, "items") == nil || DecodeExactJSON([]byte(`{"enabled":null}`), &value, "enabled") == nil {
		t.Fatal("nullable path boundary changed")
	}
}

// TestDecodeExactJSONRefusesUnsupportedShapes rejects generic/custom targets and excessive nesting
// without changing the existing permissive standard-shape decoder used by other consumers.
// TestDecodeExactJSONRefusesUnsupportedShapes отклоняет generic/custom targets и лишнюю вложенность,
// не меняя существующий decoder стандартных форм для остальных consumers.
func TestDecodeExactJSONRefusesUnsupportedShapes(t *testing.T) {
	for _, target := range []any{nil, (*exactJSONFixture)(nil), exactJSONFixture{}, &map[string]string{}, &struct {
		Raw json.RawMessage `json:"raw"`
	}{}} {
		if DecodeExactJSON([]byte(`{"raw":{}}`), target) == nil {
			t.Fatal("unsupported target accepted")
		}
	}
	for _, target := range []any{
		&struct{ exactJSONChild }{},
		&struct {
			Entries map[string]string `json:"entries,omitempty"`
		}{},
		&struct {
			Entries *map[string]string `json:"entries,omitempty"`
		}{},
		&struct {
			Entries any `json:"entries,omitempty"`
		}{},
		&struct {
			Entries *json.RawMessage `json:"entries,omitempty"`
		}{},
		&struct {
			Entries exactJSONCustomText `json:"entries,omitempty"`
		}{},
	} {
		for _, raw := range []string{`{}`, `{"entries":null}`, `{"entries":"value"}`} {
			if DecodeExactJSON([]byte(raw), target, "entries") == nil {
				t.Fatal("omitted, nullable or present unsupported shape accepted")
			}
		}
	}
	ignored := struct {
		exactJSONChild `json:"-"`
	}{}
	if DecodeExactJSON([]byte(`{}`), &ignored) != nil || DecodeExactJSON([]byte(`{"name":"promoted"}`), &ignored) == nil {
		t.Fatal("explicitly ignored embedded field changed JSON exposure")
	}
	var value exactJSONFixture
	if DecodeExactJSON([]byte(strings.Repeat(`{"nested":`, 20)+`{}`+strings.Repeat(`}`, 20)), &value) == nil {
		t.Fatal("excessive depth accepted")
	}
}
