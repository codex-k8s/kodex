package platform

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestTerminalSessionStorageDiagnosticIsClosed(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	logTerminalSessionStorageFailure(t.Context(), claimableExecution{
		runRef: "run_fixture_storage", nodeRef: "nod_fixture_storage", sessionRef: "ses_fixture_storage",
		task: "private source content must not appear", providerSecretName: "private credential locator",
	})
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"level": "WARN", "msg": runtimeCandidateEligibilityDiagnosticMessage,
		"safe_stage": "session_storage", "error_class": "TERMINAL_STORAGE",
		"run_ref": "run_fixture_storage", "node_ref": "nod_fixture_storage", "session_ref": "ses_fixture_storage",
	}
	if len(record) != len(expected)+1 || record["time"] == nil {
		t.Fatal("terminal storage diagnostic contains unexpected fields")
	}
	for key, value := range expected {
		if record[key] != value {
			t.Fatalf("terminal storage diagnostic field %s differs", key)
		}
	}
}
