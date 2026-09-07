package grpc

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/providercredential"
)

func TestCatalogDiagnosticLogHasClosedFieldsOnly(t *testing.T) {
	for _, failure := range []providercredential.ModelCatalogFailure{
		providercredential.CatalogFailureNone, providercredential.CatalogFailureUnavailable,
		providercredential.CatalogFailureUnverified, providercredential.CatalogFailureAuthorization,
		"synthetic-private-payload",
	} {
		var output bytes.Buffer
		server := &Server{}
		WithCatalogLogger(slog.New(slog.NewJSONHandler(&output, nil)))(server)
		server.logCatalogFailure(t.Context(), providercredential.ModelCatalog{Failure: failure,
			Source: "synthetic-private-payload", Models: []providercredential.CatalogModel{{ID: "synthetic-private-payload"}}})
		if failure == providercredential.CatalogFailureNone || failure == "synthetic-private-payload" {
			if output.Len() != 0 {
				t.Fatal("invalid or successful result logged as failure")
			}
			continue
		}
		var record map[string]any
		if json.Unmarshal(output.Bytes(), &record) != nil || len(record) != 5 || strings.Count(output.String(), "\n") != 1 || strings.Contains(output.String(), "synthetic") || record["stage"] != "unknown" || record["failure"] != string(failure) || record["msg"] != providerCatalogDiagnosticMessage {
			t.Fatal("unsafe catalog diagnostic log")
		}
	}
}
