package providercredential

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// expectedAPICapabilities returns the exact nine-model dated fixture expected from the embedded reviewed
// source, including explicit runtime-policy versus API defaults; it is not a live provider inventory.
// expectedAPICapabilities возвращает точную датированную fixture девяти моделей из embedded проверенного
// источника с явными runtime-policy и API defaults; это не live инвентаризация provider.
func expectedAPICapabilities() []CatalogModel {
	return []CatalogModel{
		{ID: "gpt-6.1-sol", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"low", "medium", "high", "xhigh", "max"}},
		{ID: "gpt-6-astra", DefaultReasoningEffort: "low", ReasoningEfforts: []string{"low", "medium", "high", "xhigh", "max"}},
		{ID: "gpt-6-sol", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{ID: "gpt-5.6-sol", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{ID: "gpt-5.6-terra", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{ID: "gpt-5.6-luna", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{ID: "gpt-5.5", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}},
		{ID: "gpt-5.4", DefaultReasoningEffort: "none", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}},
		{ID: "gpt-5.4-mini", DefaultReasoningEffort: "none", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}},
	}
}

// TestAPIExactNineCapabilitiesAndAccountSubsets validates the pinned source at a fixed in-window date,
// then intersects synthetic account subsets and unknown models while preserving reviewed effort/default
// values and source order.
// TestAPIExactNineCapabilitiesAndAccountSubsets проверяет закреплённый источник на фиксированную дату
// внутри окна, затем пересекает синтетические account подмножества и неизвестные модели, сохраняя
// проверенные effort/default и порядок источника.
func TestAPIExactNineCapabilitiesAndAccountSubsets(t *testing.T) {
	want := expectedAPICapabilities()
	models, err := readAPICapabilities(apiCatalogSource, apiCatalogDigest, time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC))
	if err != nil || !reflect.DeepEqual(models, want) {
		t.Fatalf("API capabilities mismatch: %v", err)
	}
	for _, subset := range [][]CatalogModel{want, want[:1], want[1:4], want[4:], {}} {
		var records []map[string]string
		for index := len(subset) - 1; index >= 0; index-- {
			records = append(records, map[string]string{"id": subset[index].ID, "object": "model"})
		}
		records = append(records, map[string]string{"id": "gpt-5.3-codex-spark", "object": "model"}, map[string]string{"id": "gpt-6-astra-unknown-snapshot", "object": "model"})
		raw, _ := json.Marshal(map[string]any{"object": "list", "data": records})
		client := catalogHTTPFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
		})
		got, err := readAPIModelCatalog(t.Context(), client, []byte("synthetic-catalog-key"))
		if err != nil || !reflect.DeepEqual(got, subset) {
			t.Fatalf("account subset changed API efforts/defaults: %v", err)
		}
	}
}

// TestAPICapabilitySourceRejectsUnverifiedSnapshot varies digest, dates, schema, revision, runtime,
// count, identities, provenance, defaults and unknown fields; even self-consistent test digests must not
// bypass semantic source checks.
// TestAPICapabilitySourceRejectsUnverifiedSnapshot меняет digest, даты, схему, ревизию, runtime, число,
// identity, происхождение, defaults и неизвестные поля; даже согласованный тестовый digest не обходит
// смысловые проверки источника.
func TestAPICapabilitySourceRejectsUnverifiedSnapshot(t *testing.T) {
	for _, mode := range []string{"digest", "future", "expired", "schema", "revision", "runtime", "interval", "missing_model", "duplicate", "source", "default_origin", "astra_api_default", "api_default", "unsupported_default", "unknown_field"} {
		t.Run(mode, func(t *testing.T) {
			var source apiCapabilitySource
			if err := json.Unmarshal(apiCatalogSource, &source); err != nil {
				t.Fatal(err)
			}
			now := source.VerifiedAt.Add(time.Hour)
			switch mode {
			case "future":
				now = source.VerifiedAt.Add(-time.Nanosecond)
			case "expired":
				now = source.ValidUntil
			case "schema":
				source.SchemaVersion++
			case "revision":
				source.Revision = "unknown"
			case "runtime":
				source.RuntimeVersion = "0.152.0"
			case "interval":
				source.ValidUntil = source.VerifiedAt.Add(apiCatalogMaxAge + time.Second)
			case "missing_model":
				source.Models = source.Models[:6]
			case "duplicate":
				source.Models[6] = source.Models[5]
			case "source":
				source.Models[1].Source = "https://example.invalid"
			case "default_origin":
				source.Models[1].DefaultOrigin = "UNKNOWN"
			case "astra_api_default":
				value := "low"
				source.Models[1].APIDefault = &value
			case "api_default":
				source.Models[0].APIDefault = nil
			case "unsupported_default":
				source.Models[0].DefaultReasoningEffort = "none"
			}
			raw, _ := json.Marshal(source)
			if mode == "unknown_field" {
				raw = append([]byte(`{"unknown":true,`), raw[1:]...)
			}
			digest := sha256.Sum256(raw)
			expected := hex.EncodeToString(digest[:])
			if mode == "digest" {
				expected = strings.Repeat("0", 64)
			}
			if models, err := readAPICapabilities(raw, expected, now); !errors.Is(err, errModelCatalogUnverified) || len(models) != 0 {
				t.Fatal("unverified source was accepted")
			}
		})
	}
}

// TestRuntimePinRejectsDifferentVersionAndOversizeOutput uses private synthetic shell executables to
// verify rejection of a different CLI version and more than 128 version-output bytes without invoking
// real Codex.
// TestRuntimePinRejectsDifferentVersionAndOversizeOutput использует приватные синтетические shell
// executable для отказа другой CLI версии и version выводу свыше 128 байтов без запуска реального Codex.
func TestRuntimePinRejectsDifferentVersionAndOversizeOutput(t *testing.T) {
	for _, output := range []string{"codex-cli 0.152.0", strings.Repeat("x", 129)} {
		root := t.TempDir()
		binary := filepath.Join(root, "fixture")
		if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' '"+output+"'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		process := AppServerProcess{binary: binary, root: root}
		if !errors.Is(process.checkCatalogRuntime(t.Context()), errModelCatalogUnverified) {
			t.Fatal("unverified runtime was accepted")
		}
	}
}
