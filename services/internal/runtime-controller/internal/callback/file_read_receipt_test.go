package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func actualFileReadResult(t *testing.T) (fileReadFixture, map[string]any, fileReadToolResult) {
	t.Helper()
	fixture := newFileReadFixture(t, []byte("PRIVATE_RECEIPT_CONTENT_CANARY-я😀"), "")
	arguments := fixture.arguments()
	result, err := fixture.server.callFileTool(t.Context(), fixture.input, runtimecontract.FileToolRead, arguments)
	if err != nil {
		t.Fatal(err)
	}
	value, ok := result.(fileReadToolResult)
	if !ok {
		t.Fatal("verified source did not produce private evidence")
	}
	return fixture, arguments, value
}

func TestReadFileReceiptRejectsUntrustedResultBeforeTerminalWrite(t *testing.T) {
	fixture, arguments, value := actualFileReadResult(t)
	wire, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	var reconstructed fileReadToolResult
	if json.Unmarshal(wire, &decoded) != nil || json.Unmarshal(wire, &reconstructed) != nil {
		t.Fatal("synthetic wire decode failed")
	}
	for _, result := range []any{nil, value.wire, decoded, string(wire), reconstructed, fileReadToolResult{wire: value.wire}} {
		if err := fixture.server.recordToolCall(t.Context(), fixture.input, runtimecontract.FileToolRead, arguments, result, nil, json.RawMessage(`"untrusted"`), time.Millisecond); err == nil {
			t.Fatal("untrusted JSON/map became successful page evidence")
		}
	}
	if len(fixture.owner.projections) != 0 || safeToolCallResult(runtimecontract.FileToolRead, value, nil) != "TOOL_UNAVAILABLE" {
		t.Fatal("invalid proof wrote a terminal event or used legacy success fallback")
	}
}

func TestReadFileReceiptRejectsBindingAndMalformedMetadata(t *testing.T) {
	mutations := []struct {
		name  string
		apply func(*runtimecontract.RunnerInput, map[string]any, *fileReadToolResult)
	}{
		{"lease", func(i *runtimecontract.RunnerInput, _ map[string]any, _ *fileReadToolResult) { i.LeaseRef += "foreign" }},
		{"fence", func(i *runtimecontract.RunnerInput, _ map[string]any, _ *fileReadToolResult) {
			i.LeaseFence += "foreign"
		}},
		{"generation", func(i *runtimecontract.RunnerInput, _ map[string]any, _ *fileReadToolResult) { i.LeaseGeneration++ }},
		{"catalog ref", func(i *runtimecontract.RunnerInput, _ map[string]any, _ *fileReadToolResult) {
			i.FileCatalog.Ref = "vfc_foreigncatalog"
		}},
		{"catalog digest", func(i *runtimecontract.RunnerInput, _ map[string]any, _ *fileReadToolResult) {
			i.FileCatalog.Digest = strings.Repeat("b", 64)
		}},
		{"purpose", func(_ *runtimecontract.RunnerInput, a map[string]any, _ *fileReadToolResult) {
			a["purpose"] = runtimecontract.FilePurposeSkill
		}},
		{"entry", func(_ *runtimecontract.RunnerInput, a map[string]any, _ *fileReadToolResult) {
			a["entry_ref"] = "vfe_foreignentry"
		}},
		{"artifact", func(_ *runtimecontract.RunnerInput, a map[string]any, _ *fileReadToolResult) {
			a["artifact_ref"] = "art_foreignartifact"
		}},
		{"revision", func(_ *runtimecontract.RunnerInput, a map[string]any, _ *fileReadToolResult) { a["revision"] = 2.0 }},
		{"source digest", func(_ *runtimecontract.RunnerInput, a map[string]any, _ *fileReadToolResult) {
			a["digest"] = "sha256:" + strings.Repeat("b", 64)
		}},
		{"unknown version", func(_ *runtimecontract.RunnerInput, _ map[string]any, r *fileReadToolResult) {
			r.evidence.receipt.Version++
		}},
		{"unknown kind", func(_ *runtimecontract.RunnerInput, _ map[string]any, r *fileReadToolResult) {
			r.evidence.receipt.Kind = "foreign"
		}},
		{"file version", func(_ *runtimecontract.RunnerInput, _ map[string]any, r *fileReadToolResult) {
			r.evidence.receipt.FileVersion++
		}},
		{"unsafe file version", func(_ *runtimecontract.RunnerInput, _ map[string]any, r *fileReadToolResult) {
			r.evidence.receipt.FileVersion = maximumFileReadReceiptInteger + 1
		}},
		{"size", func(_ *runtimecontract.RunnerInput, _ map[string]any, r *fileReadToolResult) {
			r.evidence.receipt.SizeBytes++
		}},
		{"negative offset", func(_ *runtimecontract.RunnerInput, a map[string]any, _ *fileReadToolResult) {
			a["offset_bytes"] = -1.0
		}},
		{"fractional offset", func(_ *runtimecontract.RunnerInput, a map[string]any, _ *fileReadToolResult) { a["offset_bytes"] = 0.5 }},
		{"foreign offset", func(_ *runtimecontract.RunnerInput, a map[string]any, _ *fileReadToolResult) { a["offset_bytes"] = 1.0 }},
		{"maximum", func(_ *runtimecontract.RunnerInput, a map[string]any, _ *fileReadToolResult) {
			a["maximum_bytes"] = 4.0
		}},
		{"raw caller authority", func(_ *runtimecontract.RunnerInput, a map[string]any, _ *fileReadToolResult) {
			a["headers"] = "PRIVATE_ARG_CANARY"
		}},
		{"next", func(_ *runtimecontract.RunnerInput, _ map[string]any, r *fileReadToolResult) {
			r.evidence.receipt.NextOffsetBytes++
		}},
		{"EOF", func(_ *runtimecontract.RunnerInput, _ map[string]any, r *fileReadToolResult) {
			r.evidence.receipt.EOF = false
		}},
		{"chunk digest", func(_ *runtimecontract.RunnerInput, _ map[string]any, r *fileReadToolResult) {
			r.evidence.receipt.ChunkDigest = "sha256:" + strings.Repeat("b", 64)
		}},
		{"injection", func(_ *runtimecontract.RunnerInput, _ map[string]any, r *fileReadToolResult) {
			r.evidence.receipt.EntryRef = "vfe_\nPRIVATE_RECEIPT_CANARY"
		}},
		{"oversized ref", func(_ *runtimecontract.RunnerInput, _ map[string]any, r *fileReadToolResult) {
			r.evidence.receipt.EntryRef = "vfe_" + strings.Repeat("x", 2001)
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			fixture, arguments, result := actualFileReadResult(t)
			mutation.apply(&fixture.input, arguments, &result)
			if err := fixture.server.recordToolCall(t.Context(), fixture.input, runtimecontract.FileToolRead, arguments, result, nil, json.RawMessage(`"invalid"`), time.Millisecond); err == nil || len(fixture.owner.projections) != 0 {
				t.Fatal("invalid page evidence wrote a successful terminal event")
			}
		})
	}
}

func TestReadFileReceiptPreservesWireAndContainsOnlyWhitelistedMetadata(t *testing.T) {
	fixture, arguments, result := actualFileReadResult(t)
	// Такие данные остаются только в существующем MCP ответе, не в квитанции.
	file := result.wire["file"].(map[string]any)
	file["name"], file["source"], file["download"] = "PRIVATE_NAME_CANARY", "PRIVATE_SOURCE_CANARY", map[string]any{"url": "PRIVATE_DOWNLOAD_CANARY"}
	result.wire["headers"] = map[string]any{"Authorization": "PRIVATE_HEADER_CANARY"}
	wire, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(result.wire)
	if err != nil || !bytes.Equal(wire, expected) || bytes.Contains(wire, []byte("evidence")) {
		t.Fatal("private evidence changed the MCP wire JSON")
	}
	if err := fixture.server.recordToolCall(t.Context(), fixture.input, runtimecontract.FileToolRead, arguments, result, nil, json.RawMessage(`"receipt"`), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	projection := fixture.owner.projections[0]
	var receipt map[string]any
	if json.Unmarshal([]byte(projection.GetSafeResult()), &receipt) != nil || len(receipt) != 15 || len(projection.GetSafeResult()) > maximumFileReadReceiptBytes || projection.GetState() != cp.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED {
		t.Fatal("successful projection is not bounded whitelist metadata")
	}
	for _, key := range []string{"version", "kind", "catalog_ref", "catalog_digest", "purpose", "entry_ref", "artifact_ref", "file_revision", "file_version", "size_bytes", "offset_bytes", "next_offset_bytes", "eof", "source_digest", "chunk_digest"} {
		if _, ok := receipt[key]; !ok {
			t.Fatal("receipt omitted a required metadata field")
		}
	}
	if strings.Contains(projection.GetSafeResult(), "PRIVATE_") || strings.Contains(projection.GetSafeResult(), fixture.ticket) || strings.Contains(projection.GetSafeResult(), "download") || strings.Contains(projection.GetSafeResult(), "text") || strings.Contains(projection.GetSafeResult(), "headers") {
		t.Fatal("safe terminal metadata leaked content or private transport")
	}
}

func TestReadFileReceiptFailureNeverPublishesMetadata(t *testing.T) {
	fixture, arguments, result := actualFileReadResult(t)
	for index, err := range []error{errRuntimeFileReply, context.Canceled, context.DeadlineExceeded} {
		if callErr := fixture.server.recordToolCall(t.Context(), fixture.input, runtimecontract.FileToolRead, arguments, result, err, json.RawMessage(`"failed"`), time.Millisecond); callErr != nil {
			t.Fatal(callErr)
		}
		projection := fixture.owner.projections[index]
		if projection.GetState() != cp.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED || projection.GetSafeResult() != "TOOL_UNAVAILABLE" {
			t.Fatal("failed or cancelled handler published successful page metadata")
		}
	}
}

func TestReadFileReceiptMaximumMetadataFitsExistingSafeResultBudget(t *testing.T) {
	receipt := fileReadReceipt{Version: 1, Kind: fileReadReceiptKind, CatalogRef: "vfc_" + strings.Repeat("x", 92), CatalogDigest: strings.Repeat("a", 64),
		Purpose: runtimecontract.FilePurposeWorkspaceInput, EntryRef: "vfe_" + strings.Repeat("x", 92), ArtifactRef: "art_" + strings.Repeat("x", 92),
		FileRevision: maximumFileReadReceiptInteger, FileVersion: maximumFileReadReceiptInteger, SizeBytes: runtimecontract.MaximumArtifactTransferBytes,
		OffsetBytes: runtimecontract.MaximumArtifactTransferBytes, NextOffsetBytes: runtimecontract.MaximumArtifactTransferBytes, EOF: true,
		SourceDigest: "sha256:" + strings.Repeat("a", 64), ChunkDigest: readFixtureDigest(nil)}
	raw, err := json.Marshal(receipt)
	if err != nil || len(raw) > maximumFileReadReceiptBytes {
		t.Fatal("maximum valid metadata exceeds the existing safe result budget")
	}
}
