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

func expectedAPICapabilities() []CatalogModel {
	return []CatalogModel{
		{ID: "gpt-6-astra", DefaultReasoningEffort: "low", ReasoningEfforts: []string{"low", "medium", "high", "xhigh", "max"}},
		{ID: "gpt-5.6-sol", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{ID: "gpt-5.6-terra", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{ID: "gpt-5.6-luna", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh", "max"}},
		{ID: "gpt-5.5", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}},
		{ID: "gpt-5.4", DefaultReasoningEffort: "none", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}},
		{ID: "gpt-5.4-mini", DefaultReasoningEffort: "none", ReasoningEfforts: []string{"none", "low", "medium", "high", "xhigh"}},
	}
}

func TestAPIExactSevenCapabilitiesAndAccountSubsets(t *testing.T) {
	want := expectedAPICapabilities()
	models, err := readAPICapabilities(apiCatalogSource, apiCatalogDigest, time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
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
				source.Models[0].DefaultOrigin = "UNKNOWN"
			case "astra_api_default":
				value := "low"
				source.Models[0].APIDefault = &value
			case "api_default":
				source.Models[1].APIDefault = nil
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
