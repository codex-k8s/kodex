package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestMCPStartupDiagnosticDoesNotLogErrorPayload(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	logMCPStartupFailure(t.Context(), logger, errors.New("private provider host credential input"))
	var record map[string]any
	if json.Unmarshal(output.Bytes(), &record) != nil || record["msg"] != mcpStartupFailureMessage || record["stage"] != "UNKNOWN" || strings.Contains(output.String(), "private") {
		t.Fatal("startup diagnostic leaked error or lost closed stage")
	}
	for key := range record {
		if key != "time" && key != "level" && key != "msg" && key != "stage" {
			t.Fatal("unexpected diagnostic field")
		}
	}
}
