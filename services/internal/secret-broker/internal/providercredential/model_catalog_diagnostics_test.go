package providercredential

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCatalogDiagnosticProcessStagesAndCleanup(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for account, stage := range map[string]catalogStage{
		"fixture-login":         catalogStageLogin,
		"fixture-list-schema":   catalogStageListSchema,
		"fixture-list-identity": catalogStageListIdentity,
		"fixture-cache-missing": catalogStageCacheOpen,
	} {
		t.Run(account, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			process, err := NewAppServerProcess(binary, root)
			if err != nil {
				t.Fatal(err)
			}
			auth, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]string{"access_token": "synthetic-private-payload", "account_id": account}})
			result, err := process.ObserveModelCatalog(t.Context(), auth, CatalogMethodDeviceCode)
			if err != nil || result.Failure != CatalogFailureUnverified || result.DiagnosticStage() != string(stage) || len(result.Models) != 0 || result.Source != "" {
				t.Fatal("catalog diagnostic outcome changed")
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("failed observation retained private state")
			}
		})
	}
}

func TestCatalogDiagnosticCacheBoundaries(t *testing.T) {
	now := time.Now().UTC()
	for _, test := range []struct {
		name  string
		stage catalogStage
	}{
		{"schema", catalogStageCacheSchema}, {"version", catalogStageCacheVersion},
		{"freshness", catalogStageCacheFreshness}, {"identity", catalogStageCacheIdentity},
		{"capabilities", catalogStageCacheCapabilities}, {"match", catalogStageCapabilitiesMatch},
		{"null_default", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := map[string]any{"slug": "fixture-model", "default_reasoning_level": "medium", "supported_reasoning_levels": []any{map[string]string{"effort": "medium"}}}
			snapshot := map[string]any{"fetched_at": now, "client_version": catalogCodexVersion, "models": []any{model}}
			capabilities := []CatalogModel{{ID: "fixture-model", DefaultReasoningEffort: "medium", ReasoningEfforts: []string{"medium"}}}
			switch test.name {
			case "schema":
				delete(snapshot, "models")
			case "version":
				snapshot["client_version"] = "0.153.0"
			case "freshness":
				snapshot["fetched_at"] = now.Add(-time.Hour)
			case "identity":
				model["slug"] = "invalid private payload"
			case "capabilities":
				model["default_reasoning_level"] = "unsupported"
			case "match":
				model["supported_reasoning_levels"] = []any{map[string]string{"effort": "medium"}, map[string]string{"effort": "high"}}
			case "null_default":
				model["default_reasoning_level"] = nil
				model["supported_reasoning_levels"] = []any{map[string]string{"effort": "none"}}
				capabilities[0].DefaultReasoningEffort = "none"
				capabilities[0].ReasoningEfforts = []string{"none"}
			}
			raw, _ := json.Marshal(snapshot)
			models, err := parseRemoteCodexCatalog(raw, now.Add(-time.Second), now, capabilities)
			if test.stage == "" {
				if err != nil || len(models) != 1 {
					t.Fatal("pinned null default conversion rejected")
				}
				return
			}
			result, failureErr := catalogFailure(t.Context(), err)
			if !errors.Is(err, errModelCatalogUnverified) || failureErr != nil || len(models) != 0 || result.Failure != CatalogFailureUnverified || result.DiagnosticStage() != string(test.stage) {
				t.Fatal("cache boundary diagnostic changed")
			}
		})
	}
}

func TestCatalogDiagnosticDoesNotExposeCauseOrUnknownStage(t *testing.T) {
	err := atCatalogStage(catalogStageCacheRead, errors.New("synthetic-private-payload"))
	if strings.Contains(err.Error(), "synthetic") {
		t.Fatal("raw diagnostic exposed")
	}
	result, _ := catalogFailure(t.Context(), atCatalogStage(catalogStageResult, err))
	if result.DiagnosticStage() != string(catalogStageCacheRead) {
		t.Fatal("precise stage lost")
	}
	result.stage = "synthetic-private-payload"
	if result.DiagnosticStage() != "unknown" {
		t.Fatal("open diagnostic stage")
	}
}
